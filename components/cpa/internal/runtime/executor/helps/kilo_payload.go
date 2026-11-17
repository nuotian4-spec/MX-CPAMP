package helps

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/channelscope"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	core "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	translator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
	log "github.com/sirupsen/logrus"
)

const KiloMaxRequestBytes = 20 << 20
const kiloImageTargetBytes = 19 << 20

var kiloImageSlots = make(chan struct{}, 2)

type kiloPayloadKey struct{}
type kiloImageCandidate struct {
	original string
	values   []string
}
type kiloPayloadPreparation struct {
	images         []kiloImageCandidate
	busy           bool
	busyOverBudget bool
}

// KiloPayloadError is local, request-scoped and never an upstream attempt.
type KiloPayloadError struct {
	Bytes int
	Busy  bool
}

func (e *KiloPayloadError) ErrorCode() string {
	if e.Busy {
		return "kilo_image_preparation_busy"
	}
	return "kilo_request_too_large"
}
func (e *KiloPayloadError) StatusCode() int {
	if e.Busy {
		return 503
	}
	return 413
}
func (*KiloPayloadError) IsRequestScoped() bool { return true }
func (*KiloPayloadError) NoReplay() bool        { return true }
func (e *KiloPayloadError) Error() string {
	return fmt.Sprintf(`{"error":{"code":%q,"type":"invalid_request_error","message":"Kilo request exceeds the byte budget; reduce inline images or request size","actual_bytes":%d,"limit_bytes":%d}}`, e.ErrorCode(), e.Bytes, KiloMaxRequestBytes)
}
func (e *KiloPayloadError) PublicErrorBody() string { return PresentKiloError(e).Body("") }

type kiloImageField struct{ path, value string }

// Only protocol-defined image parts are visited; strings and encrypted items are opaque.
func kiloImageFields(root ojson.Result, chat bool) []kiloImageField {
	var fields []kiloImageField
	parts := func(value ojson.Result, prefix string, chat bool) {
		for j, part := range value.Array() {
			path := prefix + "." + strconv.Itoa(j)
			if chat && part.Get("type").String() == "image_url" && part.Get("image_url.url").Type == ojson.String {
				fields = append(fields, kiloImageField{path + ".image_url.url", part.Get("image_url.url").String()})
			} else if !chat && part.Get("type").String() == "input_image" && part.Get("image_url").Type == ojson.String {
				fields = append(fields, kiloImageField{path + ".image_url", part.Get("image_url").String()})
			}
		}
	}
	if chat {
		for i, item := range root.Get("messages").Array() {
			if item.Get("role").String() == "user" {
				parts(item.Get("content"), "messages."+strconv.Itoa(i)+".content", true)
			}
		}
		return fields
	}
	for i, item := range root.Get("input").Array() {
		prefix := "input." + strconv.Itoa(i)
		kind := item.Get("type").String()
		if kind == "function_call_output" || kind == "custom_tool_call_output" {
			if item.Get("output").IsArray() {
				parts(item.Get("output"), prefix+".output", false)
			}
		} else if kind == "message" || (kind == "" && item.Get("role").Exists()) {
			parts(item.Get("content"), prefix+".content", false)
		}
	}
	return fields
}

// PrepareKiloPayload runs before history, account and egress admission.
func PrepareKiloPayload(ctx context.Context, req core.Request, opts core.Options, enabled bool) (context.Context, error) {
	if !enabled || len(req.Payload) <= KiloMaxRequestBytes {
		return ctx, nil
	}
	if err := ctx.Err(); err != nil {
		return ctx, err
	}
	chat := opts.SourceFormat == translator.FormatOpenAI
	if !chat && opts.SourceFormat != translator.FormatOpenAIResponse && opts.SourceFormat != translator.FormatCodex {
		return ctx, nil
	}
	fields := kiloImageFields(ojson.ParseBytes(req.Payload), chat)
	unique := map[string]bool{}
	var candidates []kiloImageCandidate
	removable := 0
	for _, field := range fields {
		if _, ok := kiloInlineImage(field.value); !ok {
			continue
		}
		removable += len(field.value)
		if !unique[field.value] {
			unique[field.value] = true
			candidates = append(candidates, kiloImageCandidate{original: field.value})
		}
	}
	if len(candidates) == 0 || len(req.Payload)-removable > KiloMaxRequestBytes {
		return ctx, nil
	}
	prepared := &kiloPayloadPreparation{}
	ctx = context.WithValue(ctx, kiloPayloadKey{}, prepared)
	select {
	case kiloImageSlots <- struct{}{}:
		defer func() { <-kiloImageSlots }()
	default:
		prepared.busy = true
		return ctx, nil
	}
	started := time.Now()
	sort.Slice(candidates, func(i, j int) bool { return len(candidates[i].original) > len(candidates[j].original) })
	if len(candidates) > 4 {
		candidates = candidates[:4]
	}
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return ctx, err
		}
		candidate.values = kiloReduceImage(ctx, candidate.original)
		if len(candidate.values) > 0 {
			prepared.images = append(prepared.images, candidate)
		}
	}
	log.WithFields(log.Fields{"stage": "kilo_image_prepare", "input_bytes": len(req.Payload), "image_count": len(candidates), "candidate_count": len(prepared.images), "duration_ms": time.Since(started).Milliseconds()}).Debug("Kilo payload metadata")
	return ctx, ctx.Err()
}

// ApplyKiloPayloadCandidates precedes the existing user payload rules.
func ApplyKiloPayloadCandidates(ctx context.Context, doc *ojson.Doc) {
	prepared, _ := ctx.Value(kiloPayloadKey{}).(*kiloPayloadPreparation)
	if prepared == nil {
		return
	}
	prepared.busyOverBudget = prepared.busy && doc.Len() > KiloMaxRequestBytes
	if len(prepared.images) == 0 || doc.Len() <= KiloMaxRequestBytes {
		return
	}
	fields := kiloImageFieldsFromDoc(doc)
	sort.SliceStable(prepared.images, func(i, j int) bool {
		return len(prepared.images[i].original)-len(prepared.images[i].values[0]) > len(prepared.images[j].original)-len(prepared.images[j].values[0])
	})
	for tier := 0; tier < 2; tier++ {
		for _, candidate := range prepared.images {
			if doc.Len() <= kiloImageTargetBytes {
				return
			}
			if len(candidate.values) <= tier {
				continue
			}
			for _, field := range fields {
				if field.value != candidate.original {
					continue
				}
				current := doc.Get(field.path).String()
				if current == candidate.original || (tier > 0 && current == candidate.values[tier-1]) {
					if len(candidate.values[tier]) < len(current) {
						_ = doc.Set(field.path, candidate.values[tier])
					}
				}
			}
		}
	}
}

func kiloImageFieldsFromDoc(doc *ojson.Doc) []kiloImageField {
	// The input array is read without materializing unrelated request fields.
	var fields []kiloImageField
	for i, item := range doc.Get("input").Array() {
		prefix := "input." + strconv.Itoa(i)
		kind := item.Get("type").String()
		field := "content"
		if kind == "function_call_output" || kind == "custom_tool_call_output" {
			field = "output"
		} else if kind != "message" && !(kind == "" && item.Get("role").Exists()) {
			continue
		}
		for j, part := range item.Get(field).Array() {
			if part.Get("type").String() == "input_image" && part.Get("image_url").Type == ojson.String {
				fields = append(fields, kiloImageField{prefix + "." + field + "." + strconv.Itoa(j) + ".image_url", part.Get("image_url").String()})
			}
		}
	}
	return fields
}

// Conditional rules retain their existing view of the normalized, original images.
// Model, protocol and header gates exclude rules that cannot apply to this request.
func KiloPayloadHasConditionalRules(cfg *config.Config, model string, req core.Request, opts core.Options) bool {
	if cfg == nil {
		return false
	}
	candidates := payloadModelCandidates(model, PayloadRequestedModel(opts, req.Model))
	conditional := func(entries []config.PayloadModelRule) bool {
		for _, entry := range entries {
			if len(entry.Match)+len(entry.NotMatch)+len(entry.Exist)+len(entry.NotExist) == 0 {
				continue
			}
			if protocol := strings.TrimSpace(entry.Protocol); protocol != "" && !strings.EqualFold(protocol, translator.FormatCodex.String()) {
				continue
			}
			if !payloadFromProtocolMatches(entry.FromProtocol, opts.SourceFormat.String()) || !payloadHeadersMatch(opts.Headers, entry.Headers) {
				continue
			}
			for _, candidate := range candidates {
				if matchModelPattern(strings.TrimSpace(entry.Name), candidate) {
					return true
				}
			}
		}
		return false
	}
	for _, group := range [][]config.PayloadRule{cfg.Payload.Default, cfg.Payload.DefaultRaw, cfg.Payload.Override, cfg.Payload.OverrideRaw} {
		for _, rule := range group {
			if conditional(rule.Models) {
				return true
			}
		}
	}
	for _, rule := range cfg.Payload.Filter {
		if conditional(rule.Models) {
			return true
		}
	}
	return false
}

func ValidateKiloPayloadBudget(ctx context.Context, body []byte) error {
	if !channelscope.KiloPayload(ctx) || len(body) <= KiloMaxRequestBytes {
		return nil
	}
	prepared, _ := ctx.Value(kiloPayloadKey{}).(*kiloPayloadPreparation)
	return &KiloPayloadError{Bytes: len(body), Busy: prepared != nil && prepared.busyOverBudget}
}

package helps

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/MoeForge/moego/base64"
	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/channelscope"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	core "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	translator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func TestKiloPayloadStaticWebPAndImageCount(t *testing.T) {
	raw, err := os.ReadFile("testdata/kilo-payload/static.webp")
	if err != nil {
		t.Fatal(err)
	}
	decoded, kind, err := image.Decode(bytes.NewReader(raw))
	if err != nil || kind != "webp" || decoded.Bounds().Empty() {
		t.Fatalf("static WebP fixture invalid: %s %v", kind, err)
	}
	// A valid unknown RIFF chunk makes the source larger without altering pixels.
	padding := make([]byte, 4096)
	raw = append(raw, 'J', 'U', 'N', 'K', 0, 16, 0, 0)
	raw = append(raw, padding...)
	binary.LittleEndian.PutUint32(raw[4:8], uint32(len(raw)-8))
	candidates := kiloReduceImage(context.Background(), "data:image/webp;base64,"+base64.StdEncoding.EncodeToString(raw))
	if len(candidates) == 0 || len(candidates) > 2 {
		t.Fatalf("static WebP candidates=%d", len(candidates))
	}
	var content []any
	var images []string
	removable := 0
	for i := 0; i < 6; i++ {
		img := kiloTestPNG(t, 32+i, 32, false)
		images = append(images, img)
		content = append(content, map[string]any{"type": "input_image", "image_url": img, "detail": "high"})
		removable += len(img)
	}
	content = append(content, map[string]any{"type": "input_image", "image_url": images[5], "detail": "low"})
	removable += len(images[5])
	doc := ojson.NewDocString(`{"input":[]}`)
	_ = doc.Set("input", []any{map[string]any{"role": "user", "content": content}})
	_ = doc.Set("padding", strings.Repeat("x", KiloMaxRequestBytes-removable/2))
	ctx, err := PrepareKiloPayload(kiloPayloadContext(), core.Request{Payload: doc.Bytes()}, core.Options{SourceFormat: translator.FormatOpenAIResponse}, true)
	if err != nil {
		t.Fatal(err)
	}
	prepared, _ := ctx.Value(kiloPayloadKey{}).(*kiloPayloadPreparation)
	if prepared == nil || len(prepared.images) != 4 {
		t.Fatalf("unique image budget=%+v", prepared)
	}
	seen := map[string]bool{}
	for _, candidate := range prepared.images {
		if seen[candidate.original] || len(candidate.values) == 0 || len(candidate.values) > 2 {
			t.Fatal("duplicate work or candidate tier budget violated")
		}
		seen[candidate.original] = true
	}
	ApplyKiloPayloadCandidates(ctx, doc)
	if doc.Get("input.0.content.5.image_url").String() != doc.Get("input.0.content.6.image_url").String() || doc.Get("input.0.content.6.detail").String() != "low" {
		t.Fatal("repeated URL or per-part detail changed inconsistently")
	}
}

func kiloPayloadContext() context.Context {
	return channelscope.WithKiloPayload(channelscope.With(context.Background(), "kilo"), true)
}

func kiloTestPNG(t *testing.T, width, height int, alpha bool) string {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	seed := uint32(17)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			seed = seed*1664525 + 1013904223
			a := uint8(255)
			if alpha && x%7 == 0 {
				a = 70
			}
			img.SetNRGBA(x, y, color.NRGBA{uint8(seed >> 24), uint8(seed >> 16), uint8(seed >> 8), a})
		}
	}
	var buffer bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.NoCompression}).Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buffer.Bytes())
}

func TestKiloPayloadImageFormatsAndOrientation(t *testing.T) {
	for _, alpha := range []bool{false, true} {
		input := kiloTestPNG(t, 128, 96, alpha)
		candidates := kiloReduceImage(context.Background(), input)
		if len(candidates) == 0 {
			t.Fatalf("alpha=%t no reduction", alpha)
		}
		raw, err := base64.StdEncoding.DecodeString(strings.SplitN(candidates[0], ",", 2)[1])
		if err != nil {
			t.Fatal(err)
		}
		img, format, err := image.Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		if img.Bounds().Dx() != 128 || img.Bounds().Dy() != 96 {
			t.Fatal("small image was enlarged")
		}
		if alpha && (format != "png" || img.(interface{ Opaque() bool }).Opaque()) {
			t.Fatal("alpha was lost")
		}
		if !alpha && format != "jpeg" {
			t.Fatal("opaque image is not JPEG")
		}
	}
	src := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 3; x++ {
			src.SetNRGBA(x, y, color.NRGBA{uint8(1 + x + 3*y), 0, 0, 255})
		}
	}
	for orientation := 1; orientation <= 8; orientation++ {
		out := kiloOrientImage(src, orientation)
		w, h := 3, 2
		if orientation >= 5 {
			w, h = h, w
		}
		if out.Bounds().Dx() != w || out.Bounds().Dy() != h {
			t.Fatalf("orientation=%d size=%v", orientation, out.Bounds())
		}
	}
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, src, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	exif := []byte{'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 0x01, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0}
	segment := append([]byte("Exif\x00\x00"), exif...)
	withExif := append([]byte{0xff, 0xd8, 0xff, 0xe1, byte((len(segment) + 2) >> 8), byte(len(segment) + 2)}, segment...)
	withExif = append(withExif, jpg.Bytes()[2:]...)
	if got, ok := kiloImageOrientation("jpeg", withExif); !ok || got != 6 {
		t.Fatalf("EXIF=%d known=%t", got, ok)
	}
	segment[6+18] = 9
	withExif = append([]byte{0xff, 0xd8, 0xff, 0xe1, byte((len(segment) + 2) >> 8), byte(len(segment) + 2)}, segment...)
	withExif = append(withExif, jpg.Bytes()[2:]...)
	if _, ok := kiloImageOrientation("jpeg", withExif); ok {
		t.Fatal("invalid orientation accepted")
	}
}

func TestKiloPayloadImageSkipAndBounds(t *testing.T) {
	for _, value := range []string{"https://example.invalid/image.png", "file-image-id", "data:image/gif;base64,AAAA", "data:image/png;base64,@@@@", "data:image/png;base64,AAAA"} {
		if got := kiloReduceImage(context.Background(), value); len(got) != 0 {
			t.Fatalf("unexpected reduction for %s", value)
		}
	}
	pngAnimation := append([]byte("\x89PNG\r\n\x1a\n"), []byte{0, 0, 0, 0, 'a', 'c', 'T', 'L', 0, 0, 0, 0}...)
	if !kiloAnimatedImage("png", pngAnimation) {
		t.Fatal("APNG not skipped")
	}
	webpAnimation := append([]byte("RIFF\x00\x00\x00\x00WEBP"), []byte{'V', 'P', '8', 'X', 1, 0, 0, 0, 2, 0}...)
	if !kiloAnimatedImage("webp", webpAnimation) {
		t.Fatal("animated WebP not skipped")
	}
	// DecodeConfig can reject the oversized dimensions without pixel allocation.
	raw, _ := base64.StdEncoding.DecodeString(strings.SplitN(kiloTestPNG(t, 2, 2, false), ",", 2)[1])
	binary.BigEndian.PutUint32(raw[16:20], 20001)
	binary.BigEndian.PutUint32(raw[20:24], 20001)
	binary.BigEndian.PutUint32(raw[29:33], crc32.ChecksumIEEE(raw[12:29]))
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(raw)); err != nil || cfg.Width != 20001 || cfg.Height != 20001 {
		t.Fatalf("oversized header fixture invalid: %v %v", cfg, err)
	}
	if len(kiloReduceImage(context.Background(), "data:image/png;base64,"+base64.StdEncoding.EncodeToString(raw))) != 0 {
		t.Fatal("oversized image decoded")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if len(kiloReduceImage(ctx, kiloTestPNG(t, 2, 2, false))) != 0 {
		t.Fatal("canceled image prepared")
	}
}

func TestKiloPayloadBudgetBusyAndRuleBoundary(t *testing.T) {
	ctx := kiloPayloadContext()
	for _, size := range []int{KiloMaxRequestBytes - 1, KiloMaxRequestBytes, KiloMaxRequestBytes + 1} {
		err := ValidateKiloPayloadBudget(ctx, make([]byte, size))
		if (err != nil) != (size > KiloMaxRequestBytes) {
			t.Fatalf("size=%d err=%v", size, err)
		}
	}
	big := ojson.NewDocString(`{"input":"` + strings.Repeat("x", KiloMaxRequestBytes) + `"}`)
	prepared := &kiloPayloadPreparation{busy: true}
	busyCtx := context.WithValue(ctx, kiloPayloadKey{}, prepared)
	ApplyKiloPayloadCandidates(busyCtx, big)
	var failure *KiloPayloadError
	if !errors.As(ValidateKiloPayloadBudget(busyCtx, big.Bytes()), &failure) || !failure.Busy {
		t.Fatal("busy oversize not distinguished")
	}
	small := ojson.NewDocString(`{"input":"small"}`)
	ApplyKiloPayloadCandidates(busyCtx, small)
	_ = small.Set("rule_added", strings.Repeat("x", KiloMaxRequestBytes))
	if !errors.As(ValidateKiloPayloadBudget(busyCtx, small.Bytes()), &failure) || failure.Busy {
		t.Fatal("rule expansion reported as preparation busy")
	}
	if ValidateKiloPayloadBudget(channelscope.With(context.Background(), "kilo"), big.Bytes()) != nil {
		t.Fatal("direct SDK gained HTTP budget")
	}
}

func TestKiloPayloadSlotsAndScopedFields(t *testing.T) {
	img := kiloTestPNG(t, 32, 32, false)
	doc := ojson.NewDocString(`{"input":[{"role":"user","content":[{"type":"input_text","text":"opaque"},{"type":"input_image","image_url":""}]},{"type":"reasoning","encrypted_content":"opaque"}],"unknown":{"image_url":"opaque"}}`)
	_ = doc.Set("input.0.content.1.image_url", img)
	_ = doc.Set("padding", strings.Repeat("x", KiloMaxRequestBytes-len(img)/2))
	body := doc.Bytes()
	kiloImageSlots <- struct{}{}
	kiloImageSlots <- struct{}{}
	ctx, err := PrepareKiloPayload(kiloPayloadContext(), core.Request{Payload: body}, core.Options{SourceFormat: translator.FormatOpenAIResponse}, true)
	<-kiloImageSlots
	<-kiloImageSlots
	if err != nil {
		t.Fatal(err)
	}
	prepared, _ := ctx.Value(kiloPayloadKey{}).(*kiloPayloadPreparation)
	if prepared == nil || !prepared.busy {
		t.Fatal("full slots did not return immediately")
	}
	ctx, err = PrepareKiloPayload(kiloPayloadContext(), core.Request{Payload: body}, core.Options{SourceFormat: translator.FormatOpenAIResponse}, true)
	if err != nil {
		t.Fatal(err)
	}
	out := ojson.NewDoc(body)
	ApplyKiloPayloadCandidates(ctx, out)
	if out.Get("input.0.content.1.image_url").String() == img {
		t.Fatal("eligible image unchanged")
	}
	for _, path := range []string{"input.0.content.0.text", "input.1.encrypted_content", "unknown.image_url"} {
		if out.Get(path).String() != "opaque" {
			t.Fatalf("opaque field changed: %s", path)
		}
	}
	if !bytes.Equal(body, doc.Bytes()) {
		t.Fatal("original body changed")
	}
	if len(kiloImageSlots) != 0 {
		t.Fatal("preparation slot leaked")
	}
}

func TestKiloPayloadSwitchPreservesRuntime(t *testing.T) {
	cfg := config.KiloConfig{ProxyLimit: 2}
	r, err := NewKiloRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ImageReductionEnabled = true
	if !r.Matches(cfg) {
		t.Fatal("image switch rebuilt runtime")
	}
	cfg.ProxyLimit = 3
	if r.Matches(cfg) {
		t.Fatal("protected runtime change ignored")
	}
}

func TestKiloPayloadConditionalRuleGates(t *testing.T) {
	for _, test := range []struct {
		name string
		rule config.PayloadModelRule
		want bool
	}{
		{"wildcard", config.PayloadModelRule{Name: "*", Match: []map[string]any{{"input.#(role==user).content.#(type==input_image).image_url": "original"}}}, true},
		{"not-match", config.PayloadModelRule{Name: "gpt-*", NotMatch: []map[string]any{{"input.0": "original"}}}, true},
		{"exist", config.PayloadModelRule{Name: "*", Exist: []string{"input.#.content.#.image_url"}}, true},
		{"not-exist", config.PayloadModelRule{Name: "*", NotExist: []string{"input.4"}}, true},
		{"different-model", config.PayloadModelRule{Name: "claude-*", Exist: []string{"input"}}, false},
		{"different-protocol", config.PayloadModelRule{Name: "*", Protocol: "bps", Exist: []string{"input"}}, false},
		{"different-source", config.PayloadModelRule{Name: "*", FromProtocol: "claude", Exist: []string{"input"}}, false},
		{"different-header", config.PayloadModelRule{Name: "*", Headers: map[string]string{"X-Scope": "other"}, Exist: []string{"input"}}, false},
		{"unconditional", config.PayloadModelRule{Name: "*"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := &config.Config{Payload: config.PayloadConfig{Override: []config.PayloadRule{{Models: []config.PayloadModelRule{test.rule}, Params: map[string]any{"test": true}}}}}
			got := KiloPayloadHasConditionalRules(cfg, "gpt-6.1-sol", core.Request{Model: "gpt-6.1-sol"}, core.Options{SourceFormat: translator.FormatOpenAIResponse, Headers: make(http.Header)})
			if got != test.want {
				t.Fatalf("conditional=%t want=%t", got, test.want)
			}
		})
	}
}

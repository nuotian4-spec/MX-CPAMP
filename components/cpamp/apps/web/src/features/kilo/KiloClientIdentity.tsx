import type {
  KiloClientIdentity as ClientIdentity,
  KiloClientIdentityExit,
} from '@/services/api/kilo';
import styles from './KiloAccountsPage.module.scss';

const stateLabels = { persisted: '已持久化', pending: '待初始化', invalid: '异常' };
const profileLabel = (ref: string) => `档案 ${ref.slice('profile-'.length)}`;
const exitLabel = (exit: KiloClientIdentityExit) =>
  exit.kind === 'ip' ? exit.ip : exit.kind === 'direct' ? '直连' : '旧出口关联，公网 IP 待核';

export function KiloClientIdentity({ identity }: { identity?: ClientIdentity | null }) {
  return (
    <section aria-label="客户端身份">
      <p className={styles.note}>
        客户端身份 · {identity ? stateLabels[identity.state] : '身份信息未返回'}
        {identity?.state === 'persisted' ? ` · ${identity.profiles.length} 个档案` : ''}
      </p>
      {identity?.state === 'persisted' && (
        <ul>
          {identity.profiles.map((profile) => (
            <li key={profile.ref}>
              {profileLabel(profile.ref)} ·{' '}
              {profile.exits === undefined
                ? '出口关联未返回'
                : profile.exits.length === 0
                  ? '无出口关联'
                  : profile.exits.map(exitLabel).join('、')}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

export function KiloIdentityExitLabel({
  identity,
  kind,
  ip,
}: {
  identity?: ClientIdentity | null;
  kind: 'ip' | 'direct';
  ip?: string | null;
}) {
  const profiles =
    identity?.state === 'persisted'
      ? identity.profiles.filter((profile) =>
          profile.exits?.some(
            (exit) => exit.kind === kind && (exit.kind !== 'ip' || (!!ip && exit.ip === ip))
          )
        )
      : [];
  return (
    <small>
      {profiles.length
        ? profiles.map((profile) => profileLabel(profile.ref)).join('、')
        : '档案关联待核'}
      {' · '}
      {identity ? stateLabels[identity.state] : '身份信息未返回'}
    </small>
  );
}

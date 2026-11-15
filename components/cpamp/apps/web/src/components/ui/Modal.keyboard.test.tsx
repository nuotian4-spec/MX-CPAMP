import { useState, type ReactNode } from 'react';
import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Modal } from './Modal';

vi.mock('react-dom', async (original) => ({
  ...(await original<typeof import('react-dom')>()),
  createPortal: (children: ReactNode) => children,
}));
vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));

// 真实Modal监听器使用受控节点顺序；原生Tab行为由浏览器验收确认。
class FocusNode {
  id = '';
  isConnected = true;
  disabled = false;
  tabIndex = 0;
  children: FocusNode[] = [];
  style: Record<string, string> = {};
  classList = { add: vi.fn(), remove: vi.fn() };
  focus() {
    if (!this.disabled) active = this;
  }
  hasAttribute(name: string) {
    return name === 'disabled' && this.disabled;
  }
  contains(node: FocusNode) {
    return node === this || this.children.includes(node);
  }
  querySelectorAll() {
    const dialog = renderer.root
      .findAllByProps({ role: 'dialog' })
      .find((item) => item.props['aria-labelledby'] === this.id);
    const buttons = dialog?.findAllByType('button') ?? [];
    return buttons.map((button, index) => {
      const node = (this.children[index] ??= new FocusNode());
      node.disabled = Boolean(button.props.disabled);
      return node;
    });
  }
}

let active: FocusNode;
let renderer: ReactTestRenderer;
let nodes: Map<string, FocusNode>;
let lastClose: FocusNode;
let listeners: Set<(event: KeyboardEvent) => void>;
const lowerClosed = vi.fn();
const upperClosed = vi.fn();

function Scenario({ backgroundDisabled = true, upperInitiallyOpen = true }) {
  const [lowerOpen, setLowerOpen] = useState(true);
  const [upperOpen, setUpperOpen] = useState(upperInitiallyOpen);
  return (
    <>
      <Modal
        open={lowerOpen}
        title="编辑 API 密钥"
        closeDisabled={backgroundDisabled && upperOpen}
        onClose={() => {
          lowerClosed();
          setLowerOpen(false);
        }}
        footer={<button disabled={backgroundDisabled && upperOpen}>更新</button>}
      >
        <input disabled={backgroundDisabled && upperOpen} />
      </Modal>
      <Modal
        open={upperOpen}
        title="确认差异"
        onClose={() => {
          upperClosed();
          setUpperOpen(false);
        }}
        footer={
          <>
            <button onClick={() => setUpperOpen(false)}>取消</button>
            <button>确认保存</button>
          </>
        }
      />
    </>
  );
}

describe('叠层弹窗键盘归属', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
    vi.stubGlobal('HTMLElement', FocusNode);
    nodes = new Map();
    listeners = new Set();
    const body = new FocusNode();
    active = body;
    lowerClosed.mockClear();
    upperClosed.mockClear();
    vi.stubGlobal('document', {
      body,
      documentElement: new FocusNode(),
      get activeElement() {
        return active;
      },
      querySelector: () => null,
      querySelectorAll: () =>
        renderer.root
          .findAllByProps({ role: 'dialog' })
          .map((item) => nodes.get(item.props['aria-labelledby'])),
      addEventListener: (name: string, listener: (event: KeyboardEvent) => void) => {
        if (name === 'keydown') listeners.add(listener);
      },
      removeEventListener: (name: string, listener: (event: KeyboardEvent) => void) => {
        if (name === 'keydown') listeners.delete(listener);
      },
    });
    vi.stubGlobal('window', { setTimeout, clearTimeout, scrollTo: vi.fn(), scrollY: 0 });
  });
  afterEach(async () => {
    await act(async () => renderer.unmount());
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });
  async function mount(backgroundDisabled = true, upperInitiallyOpen = true) {
    await act(async () => {
      renderer = create(
        <Scenario
          backgroundDisabled={backgroundDisabled}
          upperInitiallyOpen={upperInitiallyOpen}
        />,
        {
          createNodeMock: (element) => {
            const props = element.props as {
              className?: string;
              role?: string;
              'aria-labelledby'?: string;
              disabled?: boolean;
            };
            const node = new FocusNode();
            node.disabled = Boolean(props.disabled);
            if (props.className === 'modal-close-floating') lastClose = node;
            if (props.role === 'dialog') {
              node.id = props['aria-labelledby']!;
              node.children = [lastClose];
              nodes.set(node.id, node);
            }
            return node;
          },
        }
      );
    });
    await act(async () => {
      vi.advanceTimersByTime(0);
    });
    return renderer.root
      .findAllByProps({ role: 'dialog' })
      .map((item) => nodes.get(item.props['aria-labelledby'])!);
  }
  async function key(key: string, shiftKey = false) {
    const event = { key, shiftKey, preventDefault: vi.fn() };
    await act(async () => {
      for (const listener of [...listeners]) listener(event as unknown as KeyboardEvent);
    });
    return event;
  }
  it('禁用下层字段时，Shift+Tab从上层关闭按钮回到确认保存', async () => {
    const [, upper] = await mount();
    const controls = upper.querySelectorAll();
    controls[0].focus();
    expect((await key('Tab', true)).preventDefault).toHaveBeenCalledTimes(1);
    expect(active === controls[controls.length - 1]).toBe(true);
  });
  it('Tab从上层最后一个按钮回到上层关闭按钮', async () => {
    const [, upper] = await mount();
    const controls = upper.querySelectorAll();
    controls[controls.length - 1].focus();
    expect((await key('Tab')).preventDefault).toHaveBeenCalledTimes(1);
    expect(active === controls[0]).toBe(true);
  });
  it('上下层都可关闭时Escape只关闭上层，随后下层仍能独立关闭', async () => {
    await mount(false);
    await key('Escape');
    await act(async () => {
      vi.advanceTimersByTime(350);
    });
    expect(upperClosed).toHaveBeenCalledTimes(1);
    expect(lowerClosed).not.toHaveBeenCalled();
    expect(renderer.root.findAllByProps({ role: 'dialog' })).toHaveLength(1);
    await key('Escape');
    await act(async () => {
      vi.advanceTimersByTime(350);
    });
    expect(lowerClosed).toHaveBeenCalledTimes(1);
  });
  it('单弹窗保留Tab与Shift+Tab循环', async () => {
    const trigger = new FocusNode();
    trigger.focus();
    const [lower] = await mount(false, false);
    const controls = lower.querySelectorAll();
    controls[0].focus();
    await key('Tab', true);
    expect(active === controls[controls.length - 1]).toBe(true);
    await key('Tab');
    expect(active === controls[0]).toBe(true);
    await key('Escape');
    await act(async () => {
      vi.advanceTimersByTime(350);
    });
    expect(active === trigger).toBe(true);
  });
  it('原触发已失焦到BODY时，Escape关闭上层后回下层首个可用控件', async () => {
    const [lower] = await mount();
    await key('Escape');
    await act(async () => {
      vi.advanceTimersByTime(350);
    });
    const controls = lower.querySelectorAll();
    expect(renderer.root.findAllByProps({ role: 'dialog' })).toHaveLength(1);
    expect(active === controls[0]).toBe(true);
    await key('Tab', true);
    expect(active === controls[controls.length - 1]).toBe(true);
  });
  it('原下层弹窗已离页时保持新页面焦点', async () => {
    const [lower] = await mount();
    lower.isConnected = false;
    const nextPage = new FocusNode();
    nextPage.focus();
    await key('Escape');
    await act(async () => {
      vi.advanceTimersByTime(350);
    });
    expect(active === nextPage).toBe(true);
  });
});

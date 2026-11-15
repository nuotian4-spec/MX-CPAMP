import { act, useState, type ReactNode } from 'react';
import { create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { KiloModal } from './KiloModal';

vi.mock('react-dom', async (original) => ({
  ...(await original<typeof import('react-dom')>()),
  createPortal: (children: ReactNode) => children,
}));
vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));

// Node substitutes exercise the real Modal effects and conditional unmounts.
// Browser acceptance separately verifies actual DOM focus and event delivery.
class FocusNode {
  isConnected = true;
  disabled = false;
  tabIndex = 0;
  style: Record<string, string> = {};
  classList = { add: vi.fn(), remove: vi.fn() };
  children: FocusNode[] = [];
  parent: FocusNode | null = null;
  focus() {
    if (this.isConnected && !this.disabled) documentState.activeElement = this;
  }
  closest() {
    return this.parent;
  }
  matches() {
    return this.disabled;
  }
  hasAttribute(name: string) {
    return name === 'disabled' && this.disabled;
  }
  querySelector() {
    return this.children.find((node) => node.isConnected && !node.disabled) ?? null;
  }
  querySelectorAll() {
    return this.children;
  }
}

const body = new FocusNode();
const documentState = { activeElement: body };
let section: FocusNode;
let trigger: FocusNode;
let fallback: FocusNode;
let nestedTrigger: FocusNode;
let lastClose: FocusNode;
let listeners: Set<(event: KeyboardEvent) => void>;
let renderer: ReactTestRenderer;
let dialogNodes: Map<string, FocusNode>;

function Scenario() {
  const [open, setOpen] = useState(false);
  const [nested, setNested] = useState(false);
  return (
    <>
      <button data-testid="open" onClick={() => setOpen(true)}>
        导入账号
      </button>
      {open && (
        <KiloModal open title="导入" onClose={() => setOpen(false)}>
          <button data-testid="cancel" onClick={() => setOpen(false)}>
            取消
          </button>
          <button data-testid="nested" onClick={() => setNested(true)}>
            内部确认
          </button>
          {nested && (
            <KiloModal open title="内部" onClose={() => setNested(false)}>
              <button data-testid="nested-cancel" onClick={() => setNested(false)}>
                返回
              </button>
            </KiloModal>
          )}
        </KiloModal>
      )}
    </>
  );
}

describe('Kilo弹窗关闭生命周期', () => {
  beforeEach(async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
    vi.stubGlobal('HTMLElement', FocusNode);
    listeners = new Set();
    dialogNodes = new Map();
    section = new FocusNode();
    trigger = new FocusNode();
    fallback = new FocusNode();
    nestedTrigger = new FocusNode();
    trigger.parent = section;
    nestedTrigger.parent = section;
    section.children = [trigger, fallback];
    documentState.activeElement = body;
    vi.stubGlobal('document', {
      body,
      documentElement: new FocusNode(),
      get activeElement() {
        return documentState.activeElement;
      },
      querySelector: () => null,
      querySelectorAll: () =>
        renderer.root
          .findAllByProps({ role: 'dialog' })
          .map((dialog) => dialogNodes.get(dialog.props['aria-labelledby'])),
      addEventListener: (name: string, listener: (event: KeyboardEvent) => void) => {
        if (name === 'keydown') listeners.add(listener);
      },
      removeEventListener: (name: string, listener: (event: KeyboardEvent) => void) => {
        if (name === 'keydown') listeners.delete(listener);
      },
    });
    vi.stubGlobal('window', {
      setTimeout,
      clearTimeout,
      scrollTo: vi.fn(),
      scrollY: 0,
    });
    await act(async () => {
      renderer = create(<Scenario />, {
        createNodeMock: (element) => {
          const node = new FocusNode();
          const props = element.props as {
            className?: string;
            role?: string;
            'aria-labelledby'?: string;
          };
          if (props.className === 'modal-close-floating') lastClose = node;
          if (props.role === 'dialog') {
            node.children = [lastClose];
            dialogNodes.set(props['aria-labelledby']!, node);
          }
          return node;
        },
      });
    });
  });
  afterEach(async () => {
    await act(async () => renderer.unmount());
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });
  async function click(id: string) {
    await act(async () => renderer.root.findByProps({ 'data-testid': id }).props.onClick());
    await act(async () => {
      vi.advanceTimersByTime(0);
    });
  }
  async function open() {
    trigger.focus();
    await click('open');
    expect(documentState.activeElement === trigger).toBe(false);
  }
  async function finishClose() {
    await act(async () => {
      vi.advanceTimersByTime(350);
    });
  }
  it('条件卸载取消后回到原触发节点', async () => {
    await open();
    expect(renderer.root.findByProps({ role: 'dialog' }).props.className).toContain('opaqueModal');
    await click('cancel');
    expect(renderer.root.findAllByProps({ role: 'dialog' })).toHaveLength(0);
    expect(documentState.activeElement === trigger).toBe(true);
  });
  it('真实Modal的Escape动画关闭后回焦点并清键盘监听', async () => {
    await open();
    const preventDefault = vi.fn();
    await act(async () => {
      for (const listener of listeners)
        listener({ key: 'Escape', preventDefault } as unknown as KeyboardEvent);
    });
    await finishClose();
    expect(preventDefault).toHaveBeenCalled();
    expect(renderer.root.findAllByProps({ role: 'dialog' })).toHaveLength(0);
    expect(documentState.activeElement === trigger).toBe(true);
    expect(listeners.size).toBe(0);
  });
  it('嵌套取消回到内部触发节点，外层取消再回页内触发节点', async () => {
    await open();
    nestedTrigger.focus();
    await click('nested');
    expect(renderer.root.findAllByProps({ role: 'dialog' })).toHaveLength(2);
    await click('nested-cancel');
    expect(renderer.root.findAllByProps({ role: 'dialog' })).toHaveLength(1);
    expect(documentState.activeElement === nestedTrigger).toBe(true);
    await click('cancel');
    expect(documentState.activeElement === trigger).toBe(true);
  });
  it('删除触发节点后回同页可用控件', async () => {
    await open();
    trigger.isConnected = false;
    await click('cancel');
    expect(documentState.activeElement === fallback).toBe(true);
  });
  it('页面离开后不把焦点拉回旧连接', async () => {
    await open();
    section.isConnected = false;
    const newPage = new FocusNode();
    newPage.focus();
    await click('cancel');
    expect(documentState.activeElement === newPage).toBe(true);
  });
});

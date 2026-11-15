import { useEffect, type ComponentProps } from 'react';
import { Modal } from '@/components/ui/Modal';
import styles from './KiloAccountsPage.module.scss';

// Kilo dialogs are conditionally mounted, so closing also unmounts the Modal.
export function KiloModal(props: ComponentProps<typeof Modal>) {
  useEffect(() => {
    if (typeof document === 'undefined') return;
    const trigger = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const section = trigger?.closest('section');
    return () => {
      queueMicrotask(() => {
        if (!section?.isConnected) return;
        const target =
          trigger?.isConnected && !trigger.matches(':disabled')
            ? trigger
            : section.querySelector<HTMLElement>('button:not([disabled]), a[href], [tabindex="0"]');
        target?.focus();
      });
    };
  }, []);
  return (
    <Modal {...props} className={[styles.opaqueModal, props.className].filter(Boolean).join(' ')} />
  );
}

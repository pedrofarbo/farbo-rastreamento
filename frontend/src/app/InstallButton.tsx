import { useState } from 'react';

import { Button } from '@/components/ui/Button';
import { Modal } from '@/components/ui/Modal';
import { useToast } from '@/components/ui/Toast';

import { AddSquareIcon, InstallIcon, ShareIcon } from './icons';
import { isIos, promptInstall, usePwa } from './pwa';
import styles from './InstallButton.module.css';

/**
 * Atalho para instalar o app, no topo, enquanto ele não está instalado.
 * Android e Chrome abrem o convite do navegador; no iPhone o Safari não deixa
 * o site pedir, então o botão mostra o caminho (Compartilhar → Adicionar à
 * Tela de Início). Some quando o app já está instalado ou aberto pelo ícone.
 */
export function InstallButton({ className = '' }: { className?: string }) {
  const { installEvent, installed } = usePwa();
  const { notify } = useToast();
  const [help, setHelp] = useState(false);
  const ios = isIos();

  // Sem convite do navegador e fora do iPhone, não há o que oferecer aqui
  // (a Conta explica o caminho pelo menu do navegador).
  if (installed || (!installEvent && !ios)) return null;

  const install = async () => {
    if (!installEvent) {
      setHelp(true);
      return;
    }
    if (await promptInstall()) notify({ tone: 'success', title: 'App instalado' });
  };

  return (
    <>
      <button type="button" className={`${styles.install} ${className}`} onClick={() => void install()} aria-label="Instalar o app">
        <InstallIcon />
        <span className={styles.label}>Instalar</span>
      </button>

      <Modal
        open={help}
        title="Instalar o app no iPhone"
        onClose={() => setHelp(false)}
        footer={
          <Button variant="primary" onClick={() => setHelp(false)}>
            Entendi
          </Button>
        }
      >
        <p className={styles.intro}>Com o app na Tela de Início, ele abre pelo ícone, em tela cheia, e recebe as notificações.</p>
        <ol className={styles.steps}>
          <li>
            <span className={styles.stepIcon}>
              <ShareIcon />
            </span>
            <span>
              Toque em <strong>Compartilhar</strong>, na barra do navegador.
            </span>
          </li>
          <li>
            <span className={styles.stepIcon}>
              <AddSquareIcon />
            </span>
            <span>
              Role a lista e escolha <strong>Adicionar à Tela de Início</strong>.
            </span>
          </li>
          <li>
            <span className={`${styles.stepIcon} ${styles.stepLogo}`}>
              <img src="/assets/logo-mark.png" alt="" />
            </span>
            <span>
              Toque em <strong>Adicionar</strong> e abra o app pelo ícone da Farbo.
            </span>
          </li>
        </ol>
      </Modal>
    </>
  );
}

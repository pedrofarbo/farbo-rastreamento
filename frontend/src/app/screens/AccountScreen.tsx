import { useState } from 'react';
import { useNavigate } from 'react-router-dom';

import { meApi } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import { useToast } from '@/components/ui/Toast';
import { useAuth } from '@/stores/AuthContext';

import { BiometricCard } from '../BiometricCard';
import { clearOfflineData, isIos, promptInstall, unsubscribePush, usePwa } from '../pwa';
import styles from './Screen.module.css';

/** Conta: quem está conectado, instalar o app, painel completo e sair. */
export function AccountScreen() {
  const { user, logout } = useAuth();
  const { installEvent, installed } = usePwa();
  const { notify } = useToast();
  const navigate = useNavigate();
  const [leaving, setLeaving] = useState(false);

  const leave = async () => {
    setLeaving(true);
    try {
      // Celular compartilhado: os alertas desta conta não podem continuar
      // chegando nele depois de sair.
      const endpoint = await unsubscribePush().catch(() => null);
      if (endpoint) await meApi.unsubscribePush(endpoint).catch(() => undefined);
      clearOfflineData();
      await logout();
      navigate('/login', { replace: true });
    } finally {
      setLeaving(false);
    }
  };

  const install = async () => {
    if (await promptInstall()) notify({ tone: 'success', title: 'App instalado' });
  };

  return (
    <div className={styles.screen}>
      <div>
        <h1 className={styles.title}>Conta</h1>
        <p className={styles.lead}>{user?.name}</p>
        <p className={styles.muted}>{user?.email}</p>
      </div>

      <section className={styles.section}>
        <h2 className={styles.sectionTitle}>App no celular</h2>
        {installed ? (
          <p className={styles.good}>Você está usando o app instalado.</p>
        ) : installEvent ? (
          <>
            <p className={styles.muted}>Instale para abrir pelo ícone, em tela cheia, e receber as notificações.</p>
            <Button onClick={install} block>
              Instalar o app
            </Button>
          </>
        ) : isIos() ? (
          <p className={styles.muted}>
            No iPhone: toque em <strong>Compartilhar</strong> e depois em <strong>Adicionar à Tela de Início</strong>.
          </p>
        ) : (
          <p className={styles.muted}>
            Para instalar, abra o menu do navegador e escolha <strong>Instalar app</strong> ou{' '}
            <strong>Adicionar à tela inicial</strong>.
          </p>
        )}
      </section>

      <BiometricCard />

      <section className={styles.section}>
        <h2 className={styles.sectionTitle}>Mais</h2>
        <ul className={styles.list}>
          <li className={styles.listItem}>
            <a className={styles.link} href="/dashboard">
              Abrir o painel completo
            </a>
          </li>
          <li className={styles.listItem}>
            <a className={styles.link} href="/esqueci-senha">
              Trocar a senha
            </a>
          </li>
          <li className={styles.listItem}>
            <a className={styles.link} href="/contrato">
              Contrato de prestação de serviços
            </a>
          </li>
          <li className={styles.listItem}>
            <a className={styles.link} href="/termos-de-uso">
              Termos de Uso e Privacidade
            </a>
          </li>
        </ul>
      </section>

      <Button variant="secondary" onClick={leave} loading={leaving} block>
        Sair da conta
      </Button>
    </div>
  );
}

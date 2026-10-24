import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate } from 'react-router-dom';

import { meApi, twoFactorApi } from '@/api/resources';
import { twoFactorKey } from '@/components/auth/TwoFactorSettings';
import { Button } from '@/components/ui/Button';
import { useToast } from '@/components/ui/Toast';
import { useAuth } from '@/stores/AuthContext';

import { BiometricCard } from '../BiometricCard';
import { ChevronIcon } from '../icons';
import { clearOfflineData, isIos, promptInstall, unsubscribePush, usePwa } from '../pwa';
import styles from './Screen.module.css';

/** Conta: quem está conectado, instalar o app, painel completo e sair. */
export function AccountScreen() {
  const { user, logout, membership, isAccountOwner, paidByOrganization } = useAuth();
  const { installEvent, installed } = usePwa();
  const { notify } = useToast();
  const navigate = useNavigate();
  const [leaving, setLeaving] = useState(false);
  // A verificação em duas etapas, para o convite a ativar.
  const security = useQuery({ queryKey: twoFactorKey, queryFn: twoFactorApi.status });

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
        {membership && (
          <p className={styles.muted}>
            Conta de <strong>{membership.ownerName}</strong> · {membership.role === 'manager' ? 'Gestor' : 'Visualizador'}
            {membership.restricted && ' · alguns grupos'}
          </p>
        )}
      </div>

      <section className={styles.section}>
        <ul className={styles.list}>
          {isAccountOwner && (
            <>
              <li>
                <Link to="/conta/dados" className={styles.listLink}>
                  <span>
                    <strong>Meus dados</strong>
                    <br />
                    <span className={styles.muted}>Nome, celular, CPF e endereço de entrega</span>
                  </span>
                  <span className={styles.chevron}>
                    <ChevronIcon />
                  </span>
                </Link>
              </li>
              <li>
                <Link to="/conta/equipe" className={styles.listLink}>
                  <span>
                    <strong>Equipe</strong>
                    <br />
                    <span className={styles.muted}>Quem da sua empresa acessa os veículos da conta</span>
                  </span>
                  <span className={styles.chevron}>
                    <ChevronIcon />
                  </span>
                </Link>
              </li>
            </>
          )}
          {paidByOrganization && (
            <>
              <li>
                <Link to="/ocorrencias" className={styles.listLink}>
                  <span>
                    <strong>Ocorrências</strong>
                    <br />
                    <span className={styles.muted}>Bateu ou foi roubado: abra com as fotos e o boletim</span>
                  </span>
                  <span className={styles.chevron}>
                    <ChevronIcon />
                  </span>
                </Link>
              </li>
              <li>
                <Link to="/vistorias" className={styles.listLink}>
                  <span>
                    <strong>Vistorias</strong>
                    <br />
                    <span className={styles.muted}>As fotos do veículo que pediram a você</span>
                  </span>
                  <span className={styles.chevron}>
                    <ChevronIcon />
                  </span>
                </Link>
              </li>
            </>
          )}
          {isAccountOwner && !paidByOrganization && (
            <li>
              <Link to="/conta/indique" className={styles.listLink}>
                <span>
                  <strong>Indique e ganhe</strong>
                  <br />
                  <span className={styles.muted}>Desconto na fatura para cada amigo que virar cliente</span>
                </span>
                <span className={styles.chevron}>
                  <ChevronIcon />
                </span>
              </Link>
            </li>
          )}
          <li>
            <Link to="/manutencao" className={styles.listLink}>
              <span>
                <strong>Manutenção</strong>
                <br />
                <span className={styles.muted}>O que vence em cada veículo, por km, horas de motor ou data</span>
              </span>
              <span className={styles.chevron}>
                <ChevronIcon />
              </span>
            </Link>
          </li>
          {!paidByOrganization && (
            <li>
              <Link to="/frota" className={styles.listLink}>
                <span>
                  <strong>Frota</strong>
                  <br />
                  <span className={styles.muted}>Motoristas, custos e abastecimento, multas e locações</span>
                </span>
                <span className={styles.chevron}>
                  <ChevronIcon />
                </span>
              </Link>
            </li>
          )}
          <li>
            <Link to="/relatorios" className={styles.listLink}>
              <span>
                <strong>Relatórios</strong>
                <br />
                <span className={styles.muted}>Km, viagens, tempo parado ligado e excessos de velocidade da frota</span>
              </span>
              <span className={styles.chevron}>
                <ChevronIcon />
              </span>
            </Link>
          </li>
          <li>
            <Link to="/conta/seguranca" className={styles.listLink}>
              <span>
                <strong>Segurança</strong>
                <br />
                <span className={styles.muted}>
                  {security.data?.enabled
                    ? 'Verificação em duas etapas ativa'
                    : 'Ative a verificação em duas etapas: um código além da senha'}
                </span>
              </span>
              <span className={styles.chevron}>
                <ChevronIcon />
              </span>
            </Link>
          </li>
        </ul>
      </section>

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
            <a className={styles.link} href={paidByOrganization ? '/contrato/associado' : '/contrato'}>
              {paidByOrganization ? 'Termo de uso e comodato' : 'Contrato de prestação de serviços'}
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

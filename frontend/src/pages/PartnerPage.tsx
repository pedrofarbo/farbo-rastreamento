import { useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { Link, Navigate, NavLink, useParams } from 'react-router-dom';

import { publicApi } from '@/api/resources';
import { formatMoney, formatPercentBps } from '@/services/format';
import { periodCommissionLabel } from '@/services/commission';
import { kindPricesLabel } from '@/services/kindPrices';
import { monthLabel, referralUrl } from '@/services/referral';
import type { PartnerMonth, PartnerReport } from '@/types';

import { CustomersSection } from './partner/CustomersSection';
import { InstallationsSection } from './partner/InstallationsSection';
import { InstallPricesSection } from './partner/InstallPricesSection';
import { KitsSection } from './partner/KitsSection';
import { PasswordGate } from './partner/PasswordGate';
import { RecipientSection } from './partner/RecipientSection';
import { usePartnerReport } from './partner/usePartnerReport';
import { Tutorial } from './PartnerTutorialPage';
import styles from './Partner.module.css';
import base from './PreLaunch.module.css';

const STATUS: Record<PartnerMonth['status'], { label: string; className: string }> = {
  pending: { label: 'Aguardando fechamento', className: styles.pending },
  closed: { label: 'A pagar', className: styles.closed },
  paid: { label: 'Pago', className: styles.paid },
};

/** O valor sem o espaço que não quebra ("R$ 6,00"), para caber nos cartões. */
const money = (cents: number) => formatMoney(cents).replace(/ /g, ' ');

/** Os ícones do menu, no traço do painel. */
const ICONS: Record<string, ReactNode> = {
  home: <path d="M3 11 12 4l9 7v9a1 1 0 0 1-1 1h-5v-6H9v6H4a1 1 0 0 1-1-1v-9Z" />,
  share: (
    <>
      <circle cx="18" cy="5" r="3" />
      <circle cx="6" cy="12" r="3" />
      <circle cx="18" cy="19" r="3" />
      <path d="m8.6 13.5 6.8 4m0-11-6.8 4" />
    </>
  ),
  wallet: (
    <>
      <path d="M3 7a2 2 0 0 1 2-2h13v4" />
      <path d="M3 7v11a2 2 0 0 0 2 2h15V9H5a2 2 0 0 1-2-2Z" />
      <path d="M16 14.5h.01" />
    </>
  ),
  users: (
    <>
      <circle cx="9" cy="8" r="3.5" />
      <path d="M2 21a7 7 0 0 1 14 0m1-14.5a3.5 3.5 0 0 1 0 7M22 21a7 7 0 0 0-4-6.3" />
    </>
  ),
  calendar: <path d="M4 6h16v14H4V6Zm0 4h16M8 3v4m8-4v4" />,
  package: <path d="M21 8 12 3 3 8v8l9 5 9-5V8ZM3 8l9 5 9-5m-9 5v8" />,
  help: (
    <>
      <circle cx="12" cy="12" r="9" />
      <path d="M9.5 9a2.5 2.5 0 1 1 3.5 2.3c-.6.3-1 .9-1 1.6v.6M12 17.5v.01" />
    </>
  ),
};

function Icon({ name }: { name: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      {ICONS[name]}
    </svg>
  );
}

type SectionId = 'inicio' | 'divulgar' | 'ganhos' | 'clientes' | 'instalacoes' | 'kits' | 'tutorial';

interface Section {
  id: SectionId;
  /** No menu. */
  label: string;
  /** Na aba do navegador. */
  title: string;
  icon: string;
  /** O atalho do início: o que tem lá. */
  hint: (r: PartnerReport) => string;
  show: (r: PartnerReport) => boolean;
}

/** As seções da página, na ordem do menu; cada uma só para quem tem o que ela mostra. */
const SECTIONS: Section[] = [
  { id: 'inicio', label: 'Início', title: 'Suas indicações', icon: 'home', hint: () => '', show: () => true },
  {
    id: 'divulgar', label: 'Divulgar', title: 'Divulgar', icon: 'share',
    hint: () => 'O seu link, o QR Code e o flyer pronto.', show: () => true,
  },
  {
    id: 'ganhos', label: 'Ganhos', title: 'Seus ganhos', icon: 'wallet',
    hint: (r) => (r.recipient?.enabled ? 'A comissão mês a mês e o recebimento na sua conta.' : 'A comissão mês a mês e o que já foi pago.'),
    show: () => true,
  },
  {
    id: 'clientes', label: 'Clientes', title: 'Seus clientes', icon: 'users',
    hint: (r) => (r.protected ? 'O pagamento de cada cliente, com os veículos.' : 'Peça a senha à Farbo para ver os seus clientes.'),
    show: () => true,
  },
  {
    id: 'instalacoes', label: 'Instalações', title: 'Instalações', icon: 'calendar',
    hint: (r) => (r.manageInstalls ? 'Agendar, o link do instalador e as retiradas.' : 'Os seus valores de instalação.'),
    show: (r) => Boolean(r.manageInstalls) || r.installPrices?.mode === 'direct',
  },
  {
    id: 'kits', label: 'Kits', title: 'Seus kits', icon: 'package',
    hint: () => 'O seu estoque, as remessas e o pedido de kits.', show: (r) => Boolean(r.kits),
  },
  {
    id: 'tutorial', label: 'Como usar', title: 'Como usar', icon: 'help',
    hint: () => 'O passo a passo de tudo, para consultar quando precisar.', show: () => true,
  },
];

const pathOf = (token: string, id: SectionId) => (id === 'inicio' ? `/parceiro/${token}` : `/parceiro/${token}/${id}`);

/**
 * A página do afiliado (/parceiro/<link secreto>[/<seção>]): o menu com as
 * seções — o início (os números e os atalhos), divulgar (o link, o QR Code e
 * o flyer), os ganhos mês a mês (e o recebedor), os clientes (com a senha),
 * as instalações e os kits (para quem tem) e o tutorial. No celular, o menu
 * é a barra que rola de lado no alto; no computador, fica à esquerda. Quem
 * tem o link vê: por isso não vai para buscadores nem passa adiante no
 * Referer.
 */
export function PartnerPage() {
  const { token = '', section: param } = useParams();
  // Com a senha que a central pôs, a página pede a senha antes dos números.
  const { report, setReport, error, locked, unlock } = usePartnerReport(token);
  const current = SECTIONS.find((s) => s.id === (param ?? 'inicio'));
  const nav = useRef<HTMLElement>(null);

  useEffect(() => {
    const metas = [
      Object.assign(document.createElement('meta'), { name: 'robots', content: 'noindex, nofollow' }),
      Object.assign(document.createElement('meta'), { name: 'referrer', content: 'no-referrer' }),
    ];
    metas.forEach((m) => document.head.appendChild(m));
    return () => metas.forEach((m) => m.remove());
  }, []);

  useEffect(() => {
    document.title = `${current?.title ?? 'Suas indicações'} · Farbo Rastreadores`;
    // A seção nova começa do alto, e a barra do celular mostra o item dela.
    if (window.scrollY > 0) window.scrollTo({ top: 0 });
    const bar = nav.current;
    const active = bar?.querySelector<HTMLElement>('[aria-current="page"]');
    if (bar && active && bar.scrollWidth > bar.clientWidth) {
      bar.scrollLeft = active.offsetLeft - (bar.clientWidth - active.clientWidth) / 2;
    }
  }, [current?.id, report]);

  const header = (
    <header className={base.header}>
      <img src="/assets/logo-header.png" width={956} height={176} alt="Farbo Rastreadores" className={base.logo} />
      <span className={base.badge}>Parceiro</span>
    </header>
  );

  if (error || locked || !report) {
    return (
      <main className={base.page}>
        {header}
        {error ? (
          <p className={base.error} role="alert">
            {error}
          </p>
        ) : locked ? (
          <PasswordGate token={token} onUnlocked={unlock} />
        ) : (
          <p className={base.hint} role="status">
            Carregando…
          </p>
        )}
      </main>
    );
  }

  // Seção que não existe (ou que não é para ele): o início.
  if (!current || !current.show(report)) return <Navigate to={pathOf(token, 'inicio')} replace />;
  const sections = SECTIONS.filter((s) => s.show(report));

  return (
    <main className={`${base.page} ${styles.shell}`}>
      {header}
      <div className={styles.layout}>
        <nav ref={nav} className={styles.nav} aria-label="Seções da sua página">
          {sections.map((s) => (
            <NavLink
              key={s.id}
              to={pathOf(token, s.id)}
              end
              className={({ isActive }) => `${styles.navItem} ${isActive ? styles.navActive : ''}`}
            >
              <Icon name={s.icon} />
              {s.label}
            </NavLink>
          ))}
        </nav>
        <div className={styles.content}>
          {current.id === 'inicio' && <Home report={report} token={token} sections={sections} />}
          {current.id === 'divulgar' && <Share report={report} token={token} />}
          {current.id === 'ganhos' && (
            <Earnings report={report} token={token} onRecipient={(recipient) => setReport((r) => (r ? { ...r, recipient } : r))} />
          )}
          {current.id === 'clientes' && <Customers report={report} token={token} />}
          {current.id === 'instalacoes' && (
            <>
              <SectionTitle title="Instalações" />
              {report.installPrices?.mode === 'direct' && (
                <InstallPricesSection
                  token={token}
                  prices={report.installPrices}
                  onChange={(installPrices) => setReport((r) => (r ? { ...r, installPrices } : r))}
                />
              )}
              {report.manageInstalls && <InstallationsSection token={token} />}
            </>
          )}
          {current.id === 'kits' && <KitsSection token={token} />}
          {current.id === 'tutorial' && <Tutorial report={report} token={token} />}
        </div>
      </div>
    </main>
  );
}

function SectionTitle({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <section className={base.intro}>
      <h1 className={base.title}>{title}</h1>
      {children}
    </section>
  );
}

/** O início: quanto ele ganha, os números e os atalhos para as seções. */
function Home({ report, token, sections }: { report: PartnerReport; token: string; sections: Section[] }) {
  return (
    <>
      <section className={base.intro}>
        <h1 className={base.title}>Olá, {report.name.split(' ')[0]}!</h1>
        <p className={base.lead}>
          {report.commissionType === 'percent' ? (
            <>
              Você ganha <strong className={styles.white}>{formatPercentBps(report.commissionBps)}</strong> da mensalidade
              {report.commissionTaxBps ? ` (líquida dos impostos da Farbo, de ${formatPercentBps(report.commissionTaxBps)})` : ''}{' '}
              de cada veículo ativo dos clientes que você indicou, em cada mês em que ela é paga.
            </>
          ) : (
            <>
              Você ganha <strong className={styles.white}>{money(report.commissionCents)}</strong> por mês por veículo ativo
              dos clientes que você indicou, em cada mês em que a mensalidade dele é paga.
            </>
          )}
        </p>
        {report.planPrices && (
          <p className={base.hint}>
            Quem contrata pelo seu link paga por mês, por veículo: {kindPricesLabel(report.planPrices, money)}.
          </p>
        )}
        {report.handDelivery && (
          <p className={base.hint}>
            Você entrega o rastreador em mãos, com os kits que estão com você: quem contrata pelo seu link não paga frete.
          </p>
        )}
        {report.recurrence?.enabled && (
          <p className={base.hint}>
            Pelo seu link, também dá para pagar o semestral ({money(report.recurrence.semiannualCents)} a cada 6 meses) ou o
            anual ({money(report.recurrence.annualCents)} por ano)
            {report.equipmentModes === 'loan'
              ? ', no comodato, mais a garantia de troca de cada mês'
              : report.equipmentModes === 'sale'
                ? ', com o rastreador incluso'
                : ', com o rastreador incluso (no comodato, mais a garantia de troca de cada mês)'}
            . A sua comissão: no semestral,{' '}
            {periodCommissionLabel(report.semiannualCommission, 6)}; no anual, {periodCommissionLabel(report.annualCommission, 12)}.
          </p>
        )}
        {!report.active && <p className={base.error}>Seu link está pausado: cadastros novos não contam. Fale com a Farbo.</p>}
      </section>

      <Money report={report} />

      <section className={styles.stats} aria-label="Indicações">
        <Stat value={report.signups} label="Cadastros pelo link" />
        <Stat value={report.customers} label="Viraram clientes" />
        <Stat value={report.activeVehicles} label="Veículos ativos" />
      </section>

      <nav className={styles.shortcuts} aria-label="Atalhos">
        {sections
          .filter((s) => s.id !== 'inicio')
          .map((s) => (
            <Link key={s.id} to={pathOf(token, s.id)} className={styles.shortcut}>
              <span className={styles.shortcutIcon}>
                <Icon name={s.icon} />
              </span>
              <span className={styles.shortcutText}>
                <strong>{s.id === 'tutorial' ? 'Como usar a sua página' : s.label}</strong>
                <span>{s.hint(report)}</span>
              </span>
              <span className={styles.shortcutArrow} aria-hidden>
                ›
              </span>
            </Link>
          ))}
      </nav>

      <p className={base.hint}>Os números atualizam a cada hora. Por privacidade, aqui não aparecem os nomes de quem se cadastrou.</p>
    </>
  );
}

function Money({ report }: { report: PartnerReport }) {
  return (
    <section className={styles.money} aria-label="Valores">
      <div className={styles.moneyMain}>
        <span className={styles.label}>A receber</span>
        <strong>{money(report.toReceiveCents)}</strong>
      </div>
      <div>
        <span className={styles.label}>Já recebido</span>
        <strong>{money(report.paidCents)}</strong>
      </div>
    </section>
  );
}

/** Divulgar: o link (copiar e compartilhar), o QR Code e o flyer pronto. */
function Share({ report, token }: { report: PartnerReport; token: string }) {
  const [copied, setCopied] = useState(false);
  const link = referralUrl(report.code);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(link);
      setCopied(true);
      setTimeout(() => setCopied(false), 2500);
    } catch {
      // Sem permissão: o link está na tela para copiar à mão.
    }
  };
  const share = () => {
    void navigator
      .share?.({ title: 'Farbo Rastreadores', text: 'Garanta o preço de pré-lançamento da Farbo Rastreadores:', url: link })
      .catch(() => undefined);
  };

  return (
    <>
      <SectionTitle title="Divulgar">
        <p className={base.lead}>Quem abre o seu link e contrata vira seu cliente. Mande para quem tem veículo.</p>
        {!report.active && <p className={base.error}>Seu link está pausado: cadastros novos não contam. Fale com a Farbo.</p>}
      </SectionTitle>

      <section className={styles.linkCard} aria-label="Seu link de indicação">
        <span className={styles.label}>Seu link de indicação</span>
        <code className={styles.link}>
          {/* Quebra a linha nas barras, não no meio da palavra. */}
          {link
            .replace(/^https:\/\//, '')
            .split('/')
            .map((part, i) => (
              <span key={i}>
                {i > 0 && (
                  <>
                    /<wbr />
                  </>
                )}
                {part}
              </span>
            ))}
        </code>
        <div className={styles.linkActions}>
          <button type="button" className={base.primary} onClick={copy}>
            {copied ? 'Link copiado!' : 'Copiar link'}
          </button>
          {typeof navigator !== 'undefined' && 'share' in navigator && (
            <button type="button" className={base.secondary} onClick={share}>
              Compartilhar
            </button>
          )}
        </div>
      </section>

      {report.active && (
        <section className={styles.qrCard} aria-label="QR Code do seu link">
          <span className={styles.label}>QR Code para o seu flyer</span>
          <div className={styles.qrBox}>
            <img
              className={styles.qr}
              src={publicApi.partnerQrUrl(token, 'png')}
              width={220}
              height={220}
              alt="QR Code do seu link de indicação"
            />
          </div>
          <p className={styles.qrHint}>
            Quem aponta a câmera do celular cai direto no seu link. O flyer pronto já vem com a oferta e o seu QR, em A5
            para a gráfica, stories ou post. Só o QR, para a sua arte: no impresso, deixe com pelo menos 2,5 cm de lado e a
            borda branca em volta.
          </p>
          <div className={styles.linkActions}>
            <Link className={base.primary} to={`/parceiro/${token}/flyer`}>
              Criar flyer pronto
            </Link>
            <a className={base.secondary} href={publicApi.partnerQrUrl(token, 'png', true)} download>
              Baixar só o QR (PNG)
            </a>
            <a className={base.secondary} href={publicApi.partnerQrUrl(token, 'svg', true)} download>
              Baixar só o QR (SVG, para gráfica)
            </a>
          </div>
        </section>
      )}
    </>
  );
}

/** Os ganhos: os valores, o recebedor (com o split) e o mês a mês. */
function Earnings({
  report,
  token,
  onRecipient,
}: {
  report: PartnerReport;
  token: string;
  onRecipient: (recipient: NonNullable<PartnerReport['recipient']>) => void;
}) {
  return (
    <>
      <SectionTitle title="Seus ganhos">
        <p className={base.lead}>
          {report.recipient?.enabled
            ? 'A comissão sai no pagamento de cada cliente, pela Pagar.me, direto para a sua conta.'
            : 'No começo de cada mês, a Farbo fecha o mês anterior e paga a sua comissão por Pix.'}
        </p>
      </SectionTitle>

      <Money report={report} />

      {report.recipient?.enabled && <RecipientSection token={token} recipient={report.recipient} onChange={onRecipient} />}

      <section className={styles.months}>
        <h2 className={styles.heading}>Mês a mês</h2>
        {report.months.length === 0 ? (
          <p className={base.hint}>Ainda sem comissões. Elas aparecem quando é paga a mensalidade de um veículo de cliente indicado.</p>
        ) : (
          <ul className={styles.monthList}>
            {report.months.map((m) => (
              <li key={m.month} className={styles.month}>
                <div>
                  <strong>{monthLabel(m.month)}</strong>
                  <span className={styles.muted}>
                    {m.vehicles} {m.vehicles === 1 ? 'veículo' : 'veículos'}
                  </span>
                </div>
                <div className={styles.monthRight}>
                  <strong>{money(m.amountCents)}</strong>
                  <span className={`${styles.status} ${STATUS[m.status].className}`}>
                    {m.status === 'pending' && report.recipient?.enabled ? 'A receber' : STATUS[m.status].label}
                  </span>
                </div>
              </li>
            ))}
          </ul>
        )}
      </section>

      <p className={base.hint}>Os números atualizam a cada hora.</p>
    </>
  );
}

/** Os clientes: com a senha na página, a lista; sem, o convite para pedir a senha. */
function Customers({ report, token }: { report: PartnerReport; token: string }) {
  if (report.protected) return <CustomersSection token={token} />;
  return (
    <SectionTitle title="Seus clientes">
      <p className={base.lead}>
        Para ver os seus clientes, com os veículos e o pagamento de cada mês, peça à Farbo para colocar senha na sua página.
      </p>
    </SectionTitle>
  );
}

function Stat({ value, label }: { value: number; label: string }) {
  return (
    <div className={styles.stat}>
      <strong>{value}</strong>
      <span>{label}</span>
    </div>
  );
}

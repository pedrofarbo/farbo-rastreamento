import { useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';

import { ApiError } from '@/api/client';
import { publicApi } from '@/api/resources';
import { formatMoney } from '@/services/format';
import { monthLabel, referralUrl } from '@/services/referral';
import type { PartnerMonth, PartnerReport } from '@/types';

import styles from './Partner.module.css';
import base from './PreLaunch.module.css';

const STATUS: Record<PartnerMonth['status'], { label: string; className: string }> = {
  pending: { label: 'Aguardando fechamento', className: styles.pending },
  closed: { label: 'A pagar', className: styles.closed },
  paid: { label: 'Pago', className: styles.paid },
};

/** O valor sem o espaço que não quebra ("R$ 6,00"), para caber nos cartões. */
const money = (cents: number) => formatMoney(cents).replace(/ /g, ' ');

/**
 * A página do afiliado (/parceiro/<link secreto>): o link de indicação dele
 * (e o QR Code, para o flyer) e os números — cadastros, clientes, quanto tem
 * a receber e o que já recebeu, mês a mês. Sem dado pessoal de ninguém. Quem tem o link vê:
 * por isso não vai para buscadores nem passa adiante no Referer.
 */
export function PartnerPage() {
  const { token = '' } = useParams();
  const [report, setReport] = useState<PartnerReport | null>(null);
  const [error, setError] = useState('');
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    document.title = 'Suas indicações · Farbo Rastreadores';
    const metas = [
      Object.assign(document.createElement('meta'), { name: 'robots', content: 'noindex, nofollow' }),
      Object.assign(document.createElement('meta'), { name: 'referrer', content: 'no-referrer' }),
    ];
    metas.forEach((m) => document.head.appendChild(m));
    return () => metas.forEach((m) => m.remove());
  }, []);

  useEffect(() => {
    let alive = true;
    publicApi
      .partner(token)
      .then((r) => alive && setReport(r))
      .catch((err) => {
        if (!alive) return;
        setError(
          err instanceof ApiError && err.status === 404
            ? 'Este link não vale mais. Peça o link novo à Farbo.'
            : 'Não deu para carregar agora. Tente de novo em instantes.',
        );
      });
    return () => {
      alive = false;
    };
  }, [token]);

  const link = report ? referralUrl(report.code) : '';
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
    <main className={base.page}>
      <header className={base.header}>
        <img src="/assets/logo-header.png" width={956} height={176} alt="Farbo Rastreadores" className={base.logo} />
        <span className={base.badge}>Parceiro</span>
      </header>

      {error ? (
        <p className={base.error} role="alert">
          {error}
        </p>
      ) : !report ? (
        <p className={base.hint} role="status">
          Carregando…
        </p>
      ) : (
        <>
          <section className={base.intro}>
            <h1 className={base.title}>Olá, {report.name.split(' ')[0]}!</h1>
            <p className={base.lead}>
              Você ganha <strong className={styles.white}>{money(report.commissionCents)}</strong> por mês por veículo
              ativo dos clientes que você indicou, em cada mês em que a mensalidade dele é paga.
            </p>
            {!report.active && (
              <p className={base.error}>Seu link está pausado: cadastros novos não contam. Fale com a Farbo.</p>
            )}
          </section>

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
                Quem aponta a câmera do celular cai direto no seu link. Para a gráfica, mande o SVG: não perde qualidade
                em nenhum tamanho. No impresso, deixe o QR com pelo menos 2,5 cm de lado e a borda branca em volta.
              </p>
              <div className={styles.linkActions}>
                <a className={base.primary} href={publicApi.partnerQrUrl(token, 'png', true)} download>
                  Baixar PNG
                </a>
                <a className={base.secondary} href={publicApi.partnerQrUrl(token, 'svg', true)} download>
                  Baixar SVG (para gráfica)
                </a>
              </div>
            </section>
          )}

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

          <section className={styles.stats} aria-label="Indicações">
            <Stat value={report.signups} label="Cadastros pelo link" />
            <Stat value={report.customers} label="Viraram clientes" />
            <Stat value={report.activeVehicles} label="Veículos ativos" />
          </section>

          <section className={styles.months}>
            <h2 className={styles.heading}>Mês a mês</h2>
            {report.months.length === 0 ? (
              <p className={base.hint}>
                Ainda sem comissões. Elas aparecem quando é paga a mensalidade de um veículo de cliente indicado.
              </p>
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
                      <span className={`${styles.status} ${STATUS[m.status].className}`}>{STATUS[m.status].label}</span>
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </section>

          <p className={base.hint}>
            No começo de cada mês, fechamos o mês anterior e pagamos por Pix. Os números atualizam a cada hora.
            Por privacidade, aqui não aparecem os nomes de quem se cadastrou.
          </p>
        </>
      )}
    </main>
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

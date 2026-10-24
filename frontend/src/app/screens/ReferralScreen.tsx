import { useQuery } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';

import { meApi } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatDateOnly, formatMoney } from '@/services/format';

import { BackIcon } from '../icons';
import local from './Occurrences.module.css';
import styles from './Screen.module.css';

/** A mensagem que vai junto com o link. */
export function referralMessage(url: string): string {
  return `Uso o rastreador da Farbo e recomendo: localização em tempo real no app, bloqueio pelo celular e alerta de roubo. Faça seu pré-cadastro pelo meu link: ${url}`;
}

/**
 * Indique e ganhe: o link do cliente e o que ele rendeu. Quem se cadastra
 * pelo link e paga a primeira mensalidade vira crédito, que abate da
 * próxima fatura.
 */
export function ReferralScreen() {
  const navigate = useNavigate();
  const { notify } = useToast();
  const referral = useQuery({ queryKey: ['me', 'referral'], queryFn: meApi.referral });
  const data = referral.data;
  const p = data?.program;

  const share = async () => {
    if (!data) return;
    const text = referralMessage(data.url);
    try {
      if (navigator.share) {
        await navigator.share({ title: 'Farbo Rastreadores', text, url: data.url });
        return;
      }
      await navigator.clipboard.writeText(text);
      notify({ tone: 'success', title: 'Link copiado' });
    } catch {
      /* a pessoa cancelou */
    }
  };

  return (
    <div className={styles.screen}>
      <div className={styles.navbar}>
        <button type="button" className={styles.back} onClick={() => navigate('/conta')} aria-label="Voltar">
          <BackIcon />
        </button>
        <div className={styles.navTitle}>
          <h1>Indique e ganhe</h1>
        </div>
      </div>
      {referral.isLoading ? (
        <Spinner label="Carregando" />
      ) : referral.error || !data || !p ? (
        <p className={styles.bad}>{(referral.error as Error | null)?.message ?? 'Não deu para carregar.'}</p>
      ) : (
        <>
          <section className={local.card}>
            {p.rewardCents > 0 ? (
              <>
                <span className={local.big}>{formatMoney(p.rewardCents)}</span>
                <span className={styles.muted}>
                  de desconto na sua fatura para cada pessoa que se cadastrar pelo seu link e pagar a primeira
                  mensalidade.
                </span>
              </>
            ) : (
              <span className={styles.muted}>Mande o seu link para quem quer um rastreador.</span>
            )}
            <span className={local.link}>{data.url}</span>
            <div className={styles.row}>
              <Button variant="primary" onClick={() => void share()}>
                Compartilhar
              </Button>
              <a className={local.picker} href={`https://wa.me/?text=${encodeURIComponent(referralMessage(data.url))}`} target="_blank" rel="noreferrer noopener">
                WhatsApp
              </a>
            </div>
          </section>

          <section className={styles.section}>
            <div className={local.stats}>
              <div>
                <strong>{p.signups}</strong>
                <span className={styles.muted}>pré-cadastros</span>
              </div>
              <div>
                <strong>{p.customers}</strong>
                <span className={styles.muted}>viraram clientes</span>
              </div>
              <div>
                <strong>{formatMoney(p.earnedCents)}</strong>
                <span className={styles.muted}>{p.openCents > 0 ? `${formatMoney(p.openCents)} a abater` : 'em créditos'}</span>
              </div>
            </div>
          </section>

          {p.credits.length > 0 && (
            <section className={styles.section}>
              <h2 className={styles.sectionTitle}>Créditos</h2>
              <ul className={styles.list}>
                {p.credits.map((c, i) => (
                  <li key={i} className={styles.listItem}>
                    <span>
                      <strong>{formatMoney(c.amountCents)}</strong> · {c.referredName || 'Indicação'}
                      <br />
                      <span className={styles.muted}>
                        {formatDateOnly(c.createdAt.slice(0, 10))} ·{' '}
                        {c.remainingCents === 0 ? 'já abatido na fatura' : `${formatMoney(c.remainingCents)} a abater na próxima fatura`}
                      </span>
                    </span>
                  </li>
                ))}
              </ul>
            </section>
          )}
          <p className={styles.muted}>
            O desconto entra na próxima fatura de mensalidade (se ela já tiver Pix ou boleto gerado, na seguinte). Quem
            indicou a si mesmo não conta.
          </p>
        </>
      )}
    </div>
  );
}

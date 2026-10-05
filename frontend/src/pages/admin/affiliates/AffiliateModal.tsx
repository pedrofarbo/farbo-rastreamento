import { useEffect, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { affiliatesApi, leadsApi } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import { TextField } from '@/components/ui/Field';
import { Modal } from '@/components/ui/Modal';
import { useToast } from '@/components/ui/Toast';
import { centsToInput, formatMoney, formatPhoneInput, parseMoney } from '@/services/format';
import { partnerUrl, referralCode, referralUrl } from '@/services/referral';
import type { Affiliate, AffiliateInput } from '@/types';

import company from '../company/Company.module.css';
import styles from './Affiliates.module.css';

interface Draft {
  name: string;
  handle: string;
  code: string;
  commission: string;
  email: string;
  phone: string;
  pixKey: string;
  notes: string;
  active: boolean;
}

function draftOf(a: Affiliate | null, defaultCents: number): Draft {
  return {
    name: a?.name ?? '',
    handle: a?.handle ?? '',
    code: a?.code ?? '',
    commission: centsToInput(a?.commissionCents ?? defaultCents),
    email: a?.email ?? '',
    phone: a?.phone ?? '',
    pixKey: a?.pixKey ?? '',
    notes: a?.notes ?? '',
    active: a?.active ?? true,
  };
}

/** O código que o servidor vai usar: o digitado, ou o do @, ou o do nome. */
export function codeFor(d: Pick<Draft, 'code' | 'handle' | 'name'>): string {
  return referralCode(d.code.trim() || d.handle.replace(/^@/, '') || d.name);
}

/**
 * Cadastro do afiliado e, depois de criado, os links: o de cadastro (com o
 * QR Code) e o da página dele com os números.
 */
export function AffiliateModal({
  affiliate,
  open,
  defaultCents,
  onClose,
  onSaved,
}: {
  /** null: novo afiliado. */
  affiliate: Affiliate | null;
  open: boolean;
  defaultCents: number;
  onClose: () => void;
  onSaved: (a: Affiliate) => void;
}) {
  const { notify } = useToast();
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState<Draft>(() => draftOf(affiliate, defaultCents));
  const [error, setError] = useState('');
  const [renewing, setRenewing] = useState(false);

  useEffect(() => {
    if (open) {
      setDraft(draftOf(affiliate, defaultCents));
      setError('');
      setRenewing(false);
    }
    // Só ao abrir (ou ao trocar de afiliado): salvar não apaga o que está na tela.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, affiliate?.id]);

  const set = <K extends keyof Draft>(key: K, value: Draft[K]) => setDraft((d) => ({ ...d, [key]: value }));
  const code = codeFor(draft);

  const save = useMutation({
    mutationFn: (input: AffiliateInput) =>
      affiliate ? affiliatesApi.update(affiliate.id, input) : affiliatesApi.create(input),
    onSuccess: (saved) => {
      queryClient.invalidateQueries({ queryKey: ['affiliates'] });
      notify({
        tone: 'success',
        title: affiliate ? 'Afiliado atualizado' : 'Afiliado criado',
        description: affiliate ? undefined : 'Copie o link de cadastro e o da página dele para mandar ao afiliado.',
      });
      onSaved(saved);
    },
    onError: (err: Error) => setError(err.message),
  });

  const renew = useMutation({
    mutationFn: () => affiliatesApi.renewReportLink(affiliate!.id),
    onSuccess: (saved) => {
      queryClient.invalidateQueries({ queryKey: ['affiliates'] });
      notify({ tone: 'success', title: 'Link da página trocado', description: 'Mande o link novo ao afiliado: o antigo não abre mais.' });
      setRenewing(false);
      onSaved(saved);
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não foi possível trocar o link', description: err.message }),
  });

  const submit = () => {
    setError('');
    const cents = draft.commission.trim() === '' ? null : parseMoney(draft.commission);
    if (draft.commission.trim() !== '' && cents === null) {
      setError('Valor por cliente inválido.');
      return;
    }
    save.mutate({
      name: draft.name,
      handle: draft.handle,
      code: draft.code,
      commissionCents: cents,
      email: draft.email,
      phone: draft.phone,
      pixKey: draft.pixKey,
      notes: draft.notes,
      active: draft.active,
    });
  };

  const copy = async (text: string, what: string) => {
    try {
      await navigator.clipboard.writeText(text);
      notify({ tone: 'success', title: `${what} copiado`, description: text });
    } catch {
      notify({ tone: 'error', title: 'Não deu para copiar', description: text });
    }
  };

  const qr = (format: 'svg' | 'png') =>
    leadsApi
      .downloadQr(referralUrl(affiliate!.code), `indicacao-${affiliate!.code}`, format)
      .catch((err: Error) => notify({ tone: 'error', title: 'Não foi possível baixar', description: err.message }));

  const codeChanged = affiliate !== null && code !== affiliate.code;

  return (
    <Modal
      open={open}
      wide
      title={affiliate ? affiliate.name : 'Novo afiliado'}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {affiliate ? 'Fechar' : 'Cancelar'}
          </Button>
          <Button variant="primary" loading={save.isPending} disabled={!draft.name.trim()} onClick={submit}>
            {affiliate ? 'Salvar' : 'Criar afiliado'}
          </Button>
        </>
      }
    >
      <div className={styles.modal}>
        {affiliate && (
          <section className={styles.links} aria-label="Links do afiliado">
            <div className={styles.linkBox}>
              <span className={styles.linkLabel}>Link de cadastro (para divulgar)</span>
              <code className={styles.linkText}>{referralUrl(affiliate.code)}</code>
              <div className={styles.linkButtons}>
                <Button size="small" variant="secondary" onClick={() => void copy(referralUrl(affiliate.code), 'Link de cadastro')}>
                  Copiar link
                </Button>
                <Button size="small" variant="ghost" onClick={() => void qr('png')}>
                  QR Code (PNG)
                </Button>
                <Button size="small" variant="ghost" onClick={() => void qr('svg')}>
                  QR Code (SVG)
                </Button>
              </div>
            </div>
            <div className={styles.linkBox}>
              <span className={styles.linkLabel}>Página do afiliado (só para ele: mostra os números)</span>
              <code className={styles.linkText}>{partnerUrl(affiliate.reportToken)}</code>
              <div className={styles.linkButtons}>
                <Button size="small" variant="secondary" onClick={() => void copy(partnerUrl(affiliate.reportToken), 'Link da página')}>
                  Copiar link
                </Button>
                <a className={styles.open} href={partnerUrl(affiliate.reportToken)} target="_blank" rel="noopener noreferrer">
                  Abrir
                </a>
                {renewing ? (
                  <span className={styles.confirm}>
                    O link atual para de abrir.
                    <Button size="small" variant="danger" loading={renew.isPending} onClick={() => renew.mutate()}>
                      Trocar
                    </Button>
                    <Button size="small" variant="ghost" onClick={() => setRenewing(false)}>
                      Não
                    </Button>
                  </span>
                ) : (
                  <Button size="small" variant="ghost" onClick={() => setRenewing(true)}>
                    Gerar link novo
                  </Button>
                )}
              </div>
            </div>
            <p className={company.summary}>
              <span>
                Cadastros pelo link <strong>{affiliate.signups}</strong>
              </span>
              <span>
                Clientes <strong>{affiliate.activeCustomers}</strong> ativos de <strong>{affiliate.customers}</strong>
              </span>
              <span>
                A fechar <strong>{formatMoney(affiliate.pendingCents)}</strong>
              </span>
              <span>
                A pagar <strong>{formatMoney(affiliate.openCents)}</strong>
              </span>
              <span>
                Pago <strong>{formatMoney(affiliate.paidCents)}</strong>
              </span>
            </p>
          </section>
        )}

        <div className={company.grid}>
          <TextField label="Nome" value={draft.name} maxLength={120} onChange={(e) => set('name', e.target.value)} autoFocus={!affiliate} />
          <TextField
            label="@ do Instagram"
            value={draft.handle}
            maxLength={31}
            placeholder="@fulano.moto"
            onChange={(e) => set('handle', e.target.value)}
            hint="Aparece na tela de cadastro: “Indicado por @fulano”."
          />
          <TextField
            label="Código do link"
            value={draft.code}
            maxLength={40}
            placeholder={codeFor({ ...draft, code: '' }) || 'sai do @ ou do nome'}
            onChange={(e) => set('code', e.target.value)}
            hint={
              codeChanged ? (
                <span className={company.danger}>Mudar o código muda o link: o antigo para de funcionar.</span>
              ) : (
                <>Link: {code ? referralUrl(code).replace(/^https:\/\//, '') : '—'}</>
              )
            }
          />
          <TextField
            label="Valor por cliente, por mês (R$)"
            value={draft.commission}
            inputMode="decimal"
            onChange={(e) => set('commission', e.target.value)}
            hint={
              affiliate
                ? 'O valor novo vale para os meses ainda não pagos pelos clientes.'
                : `Padrão: ${formatMoney(defaultCents)}. Conta em cada mês em que o cliente indicado paga a mensalidade.`
            }
          />
          <TextField label="E-mail" type="email" value={draft.email} maxLength={254} onChange={(e) => set('email', e.target.value)} />
          <TextField
            label="WhatsApp"
            type="tel"
            value={draft.phone}
            maxLength={16}
            onChange={(e) => set('phone', formatPhoneInput(e.target.value, draft.phone))}
          />
          <TextField label="Chave Pix" value={draft.pixKey} maxLength={140} onChange={(e) => set('pixKey', e.target.value)} hint="Vai na conta a pagar do fechamento." />
        </div>
        <label className={company.check}>
          <input type="checkbox" checked={draft.active} onChange={(e) => set('active', e.target.checked)} />
          Ativo (o link indica e os meses pagos rendem comissão)
        </label>
        <textarea
          className={company.textarea}
          aria-label="Observações"
          placeholder="Observações (combinado, contrato, contato do empresário...)"
          maxLength={2000}
          value={draft.notes}
          onChange={(e) => set('notes', e.target.value)}
        />
        {error && (
          <p className={company.danger} role="alert">
            {error}
          </p>
        )}
      </div>
    </Modal>
  );
}

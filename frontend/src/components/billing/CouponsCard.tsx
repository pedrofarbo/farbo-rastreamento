import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { couponsApi } from '@/api/resources';
import { Badge } from '@/components/ui/Badge';
import type { BadgeTone } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { SelectField, TextField } from '@/components/ui/Field';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { centsToInput, formatDateOnly, formatMoney, parseMoney, todayISO } from '@/services/format';
import type { Coupon, CouponInput } from '@/types';

import pageStyles from '@/pages/Page.module.css';
import styles from './Billing.module.css';

const key = ['coupons'] as const;

/** "10%" ou "R$ 20,00". */
export function couponDiscount(c: Pick<Coupon, 'percent' | 'amountCents'>): string {
  return c.percent ? `${c.percent}%` : formatMoney(c.amountCents ?? 0);
}

/** "na 1ª cobrança" ou "nas 3 primeiras". */
export function couponCharges(charges: number): string {
  return charges === 1 ? 'na 1ª cobrança' : `nas ${charges} primeiras`;
}

/** A situação do cupom hoje: valendo, desligado, vencido ou esgotado. */
export function couponStatus(c: Coupon, today = todayISO()): { label: string; tone: BadgeTone } {
  if (!c.active) return { label: 'Desligado', tone: 'neutral' };
  if (c.validUntil && c.validUntil < today) return { label: 'Vencido', tone: 'warning' };
  if (c.maxUses !== null && c.uses >= c.maxUses) return { label: 'Esgotado', tone: 'warning' };
  return { label: 'Valendo', tone: 'success' };
}

/**
 * Os cupons de desconto da contratação direta pelo site (central): o desconto (porcentagem ou valor
 * fixo) sai da mensalidade nas primeiras cobranças da assinatura; pelo link de um representante, não
 * vale. Sem nenhum valendo, o site nem mostra o campo. O cupom usado não sai: desliga.
 */
export function CouponsCard() {
  const coupons = useQuery({ queryKey: key, queryFn: couponsApi.list });
  // O cupom aberto no diálogo: null fechado; 'new' o novo.
  const [open, setOpen] = useState<Coupon | 'new' | null>(null);
  const list = coupons.data ?? [];
  return (
    <Card
      title="Cupons de desconto"
      subtitle="Na contratação direta pelo site (/contratar), sem o link de um representante: o desconto sai da mensalidade nas primeiras cobranças e o valor normal volta sozinho. Sem nenhum cupom valendo, o site não mostra o campo."
      actions={
        <Button variant="primary" size="small" onClick={() => setOpen('new')}>
          Novo cupom
        </Button>
      }
    >
      {coupons.isLoading ? (
        <Spinner label="Carregando" />
      ) : coupons.isError ? (
        <div className={pageStyles.note}>Não deu para carregar os cupons agora.</div>
      ) : list.length === 0 ? (
        <p className={pageStyles.description}>
          Nenhum cupom ainda. Crie um e divulgue o código, ou o link /contratar?cupom=CÓDIGO, que já abre com ele.
        </p>
      ) : (
        <div className={pageStyles.tableWrap}>
          <table className={pageStyles.table}>
            <thead>
              <tr>
                <th>Código</th>
                <th>Desconto</th>
                <th>Validade</th>
                <th>Usos</th>
                <th>Situação</th>
                <th aria-label="Ações" />
              </tr>
            </thead>
            <tbody>
              {list.map((c) => {
                const status = couponStatus(c);
                return (
                  <tr key={c.id}>
                    <td>
                      <strong>{c.code}</strong>
                      {c.description && (
                        <>
                          <br />
                          <span className={styles.muted}>{c.description}</span>
                        </>
                      )}
                    </td>
                    <td>
                      {couponDiscount(c)} {couponCharges(c.charges)}
                    </td>
                    <td>{c.validUntil ? `até ${formatDateOnly(c.validUntil)}` : 'sem validade'}</td>
                    <td>{c.maxUses !== null ? `${c.uses} de ${c.maxUses}` : c.uses}</td>
                    <td>
                      <Badge tone={status.tone}>{status.label}</Badge>
                    </td>
                    <td>
                      <Button size="small" onClick={() => setOpen(c)}>
                        Editar
                      </Button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      {open !== null && <CouponModal coupon={open === 'new' ? null : open} onClose={() => setOpen(null)} />}
    </Card>
  );
}

interface Draft {
  code: string;
  description: string;
  kind: 'percent' | 'amount';
  value: string;
  charges: string;
  validUntil: string;
  maxUses: string;
  active: boolean;
}

function draftOf(c: Coupon | null): Draft {
  if (!c) {
    return { code: '', description: '', kind: 'percent', value: '', charges: '1', validUntil: '', maxUses: '', active: true };
  }
  return {
    code: c.code,
    description: c.description,
    kind: c.percent ? 'percent' : 'amount',
    value: c.percent ? String(c.percent) : centsToInput(c.amountCents ?? 0),
    charges: String(c.charges),
    validUntil: c.validUntil ?? '',
    maxUses: c.maxUses !== null ? String(c.maxUses) : '',
    active: c.active,
  };
}

/** O rascunho como a API recebe; ou o problema, em texto. */
export function couponInput(d: Draft): CouponInput | string {
  const code = d.code.replace(/\s+/g, '').toUpperCase();
  if (!/^[A-Z0-9_-]{3,30}$/.test(code)) return 'O código tem de 3 a 30 letras, números, - ou _ (sem espaços nem acentos).';
  let percent: number | null = null;
  let amountCents: number | null = null;
  if (d.kind === 'percent') {
    percent = Number(d.value.replace('%', '').trim());
    if (!Number.isInteger(percent) || percent < 1 || percent > 100) return 'A porcentagem vai de 1 a 100.';
  } else {
    amountCents = parseMoney(d.value);
    if (amountCents === null || amountCents < 1) return 'Valor inválido: use, por exemplo, 20,00.';
  }
  const charges = Number(d.charges);
  if (!Number.isInteger(charges) || charges < 1 || charges > 60) return 'O desconto vale de 1 a 60 cobranças.';
  let maxUses: number | null = null;
  if (d.maxUses.trim()) {
    maxUses = Number(d.maxUses);
    if (!Number.isInteger(maxUses) || maxUses < 1) return 'O limite de usos é um número a partir de 1 (vazio: sem limite).';
  }
  return {
    code,
    description: d.description.trim(),
    percent,
    amountCents,
    charges,
    validUntil: d.validUntil || null,
    maxUses,
    active: d.active,
  };
}

function CouponModal({ coupon, onClose }: { coupon: Coupon | null; onClose: () => void }) {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [draft, setDraft] = useState<Draft>(() => draftOf(coupon));
  const [error, setError] = useState('');
  const set = <K extends keyof Draft>(field: K, value: Draft[K]) => setDraft((d) => ({ ...d, [field]: value }));
  const done = (title: string) => {
    void queryClient.invalidateQueries({ queryKey: key });
    notify({ tone: 'success', title });
    onClose();
  };
  const save = useMutation({
    mutationFn: () => {
      const input = couponInput(draft);
      if (typeof input === 'string') throw new Error(input);
      if (!coupon) return couponsApi.create(input);
      const { code: _code, ...changes } = input;
      return couponsApi.update(coupon.id, changes);
    },
    onSuccess: () => done(coupon ? 'Cupom salvo' : 'Cupom criado'),
    onError: (err: Error) => setError(err.message),
  });
  const remove = useMutation({
    mutationFn: () => couponsApi.remove((coupon as Coupon).id),
    onSuccess: () => done('Cupom excluído'),
    onError: (err: Error) => setError(err.message),
  });
  const code = draft.code.replace(/\s+/g, '').toUpperCase();
  return (
    <Modal
      open
      title={coupon ? `Cupom ${coupon.code}` : 'Novo cupom'}
      onClose={onClose}
      footer={
        <>
          {coupon && coupon.uses === 0 && (
            <Button variant="danger" loading={remove.isPending} onClick={() => remove.mutate()}>
              Excluir
            </Button>
          )}
          <Button variant="ghost" onClick={onClose}>
            Cancelar
          </Button>
          <Button variant="primary" loading={save.isPending} disabled={!code} onClick={() => save.mutate()}>
            {coupon ? 'Salvar' : 'Criar cupom'}
          </Button>
        </>
      }
    >
      <div className={pageStyles.form}>
        {error && (
          <div className={pageStyles.note} role="alert">
            {error}
          </div>
        )}
        <div className={pageStyles.formRow}>
          <TextField
            label="Código"
            autoCapitalize="characters"
            maxLength={30}
            placeholder="BEMVINDO"
            disabled={!!coupon}
            hint={coupon ? 'O código não muda depois de criado.' : 'Letras, números, - ou _.'}
            value={draft.code}
            onChange={(e) => set('code', e.target.value.toUpperCase())}
          />
          <TextField
            label="Descrição (só a central vê)"
            maxLength={200}
            placeholder="Ex.: campanha do Instagram"
            value={draft.description}
            onChange={(e) => set('description', e.target.value)}
          />
        </div>
        <div className={pageStyles.formRow}>
          <SelectField label="Desconto em" value={draft.kind} onChange={(e) => set('kind', e.target.value as Draft['kind'])}>
            <option value="percent">Porcentagem</option>
            <option value="amount">Valor fixo (R$)</option>
          </SelectField>
          <TextField
            label={draft.kind === 'percent' ? 'Porcentagem (%)' : 'Valor (R$)'}
            inputMode={draft.kind === 'percent' ? 'numeric' : 'decimal'}
            placeholder={draft.kind === 'percent' ? '10' : '20,00'}
            hint={draft.kind === 'amount' ? 'Abatido de cada cobrança com desconto, sem passar de zero.' : undefined}
            value={draft.value}
            onChange={(e) => set('value', e.target.value)}
          />
          <TextField
            label="Nas primeiras (cobranças)"
            inputMode="numeric"
            hint="1: só a primeira. No semestral e no anual, cada cobrança é um período."
            value={draft.charges}
            onChange={(e) => set('charges', e.target.value.replace(/\D/g, '').slice(0, 2))}
          />
        </div>
        <div className={pageStyles.formRow}>
          <TextField
            label="Válido até (opcional)"
            type="date"
            hint="Inclusive. Vazio: sem validade."
            value={draft.validUntil}
            onChange={(e) => set('validUntil', e.target.value)}
          />
          <TextField
            label="Limite de usos (opcional)"
            inputMode="numeric"
            hint={coupon ? `Já usado ${coupon.uses} ${coupon.uses === 1 ? 'vez' : 'vezes'}. Vazio: sem limite.` : 'Contratações. Vazio: sem limite.'}
            value={draft.maxUses}
            onChange={(e) => set('maxUses', e.target.value.replace(/\D/g, '').slice(0, 7))}
          />
        </div>
        <label className={styles.promoOption}>
          <input type="checkbox" checked={draft.active} onChange={(e) => set('active', e.target.checked)} />
          <span>
            <strong>Valendo</strong>
            <br />
            Desligado, o site recusa o código. Quem já usou segue com o desconto que ganhou, mesmo que você mude o cupom.
          </span>
        </label>
        {code && (
          <p className={pageStyles.description}>
            Link que já abre com o cupom: {window.location.origin}/contratar?cupom={encodeURIComponent(code)}
          </p>
        )}
      </div>
    </Modal>
  );
}

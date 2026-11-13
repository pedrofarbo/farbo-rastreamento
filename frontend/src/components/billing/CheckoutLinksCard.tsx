import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { checkoutLinksApi } from '@/api/resources';
import { Badge } from '@/components/ui/Badge';
import type { BadgeTone } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { SelectField, TextField } from '@/components/ui/Field';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { SITE_URL } from '@/config/landing';
import { centsToInput, formatDateOnly, formatMoney, parseMoney, todayISO } from '@/services/format';
import type { CheckoutLink, CheckoutLinkInput } from '@/types';

import pageStyles from '@/pages/Page.module.css';
import styles from './Billing.module.css';

const key = ['checkout-links'] as const;

/** O endereço que a central manda a quem vai contratar. */
export function checkoutLinkUrl(token: string): string {
  return `${SITE_URL}/contratar?link=${token}`;
}

/** "Comodato · anual · R$ 600,00/ano" ou "Incluso · mensal · R$ 49,90/mês". */
export function checkoutLinkOffer(l: Pick<CheckoutLink, 'equipmentMode' | 'periodMonths' | 'priceCents'>): string {
  const period = l.periodMonths === 12 ? 'anual' : l.periodMonths === 6 ? 'semestral' : 'mensal';
  const per = l.periodMonths === 12 ? '/ano' : l.periodMonths === 6 ? '/semestre' : '/mês';
  return `${l.equipmentMode === 'loan' ? 'Comodato' : 'Incluso'} · ${period} · ${formatMoney(l.priceCents)}${per}`;
}

/** "Farbo, com bloqueio · R$ 100,00", "Farbo, sem bloqueio · sem custo" ou "Por conta própria". */
export function checkoutLinkInstall(
  l: Pick<CheckoutLink, 'equipmentMode' | 'farboInstall' | 'installCents' | 'installBlocking' | 'uninstallCents'>,
): string {
  if (l.equipmentMode === 'sale' && !l.farboInstall) return 'Por conta própria';
  const install = `Farbo, ${l.installBlocking ? 'com' : 'sem'} bloqueio · ${l.installCents > 0 ? formatMoney(l.installCents) : 'sem custo'}`;
  return l.equipmentMode === 'loan' && l.uninstallCents > 0 ? `${install}; desinstalação ${formatMoney(l.uninstallCents)}` : install;
}

/** A situação do link hoje: valendo, desligado, vencido, usado (o de um uso só) ou esgotado. */
export function checkoutLinkStatus(l: CheckoutLink, today = todayISO()): { label: string; tone: BadgeTone } {
  if (!l.active) return { label: 'Desligado', tone: 'neutral' };
  if (l.validUntil && l.validUntil < today) return { label: 'Vencido', tone: 'warning' };
  if (l.maxUses !== null && l.uses >= l.maxUses) return l.maxUses === 1 ? { label: 'Usado', tone: 'accent' } : { label: 'Esgotado', tone: 'warning' };
  return { label: 'Valendo', tone: 'success' };
}

/**
 * Os links de pagamento (central): cada link é uma oferta fechada — o rastreador incluso ou em
 * comodato, o mensal, o semestral ou o anual, o valor, quem instala (com ou sem bloqueio) e as
 * taxas. Quem abre o link só informa o veículo e os dados e paga. O link usado não sai: desliga.
 */
export function CheckoutLinksCard() {
  const links = useQuery({ queryKey: key, queryFn: checkoutLinksApi.list });
  const { notify } = useToast();
  // O link aberto no diálogo: null fechado; 'new' o novo.
  const [open, setOpen] = useState<CheckoutLink | 'new' | null>(null);
  const list = links.data ?? [];
  const copy = async (token: string) => {
    try {
      await navigator.clipboard.writeText(checkoutLinkUrl(token));
      notify({ tone: 'success', title: 'Link copiado' });
    } catch {
      notify({ tone: 'error', title: 'Não deu para copiar', description: checkoutLinkUrl(token) });
    }
  };
  return (
    <Card
      title="Links de pagamento"
      subtitle="Uma oferta fechada por link: o rastreador, a recorrência, o valor, a instalação e as taxas. Quem abre o link só informa o veículo e os dados e paga, também com a contratação pelo site fechada. Não vale com cupom nem com o representante, e o próximo veículo do cliente sai no preço normal."
      actions={
        <Button variant="primary" size="small" onClick={() => setOpen('new')}>
          Novo link
        </Button>
      }
    >
      {links.isLoading ? (
        <Spinner label="Carregando" />
      ) : links.isError ? (
        <div className={pageStyles.note}>Não deu para carregar os links agora.</div>
      ) : list.length === 0 ? (
        <p className={pageStyles.description}>
          Nenhum link ainda. Crie um com as condições combinadas e mande para quem vai contratar.
        </p>
      ) : (
        <div className={pageStyles.tableWrap}>
          <table className={`${pageStyles.table} ${pageStyles.stackTable}`}>
            <thead>
              <tr>
                <th>Link</th>
                <th>Oferta</th>
                <th>Instalação</th>
                <th>Usos</th>
                <th>Situação</th>
                <th aria-label="Ações" />
              </tr>
            </thead>
            <tbody>
              {list.map((l) => {
                const status = checkoutLinkStatus(l);
                return (
                  <tr key={l.id}>
                    <td>
                      <strong>{l.label}</strong>
                      <br />
                      <span className={styles.muted}>{l.validUntil ? `até ${formatDateOnly(l.validUntil)}` : 'sem validade'}</span>
                    </td>
                    <td data-label="Oferta">{checkoutLinkOffer(l)}</td>
                    <td data-label="Instalação">{checkoutLinkInstall(l)}</td>
                    <td data-label="Usos">{l.maxUses !== null ? `${l.uses} de ${l.maxUses}` : l.uses}</td>
                    <td data-label="Situação">
                      <Badge tone={status.tone}>{status.label}</Badge>
                    </td>
                    <td>
                      <div className={pageStyles.actions}>
                        <Button size="small" variant="ghost" onClick={() => void copy(l.token)}>
                          Copiar link
                        </Button>
                        <Button size="small" onClick={() => setOpen(l)}>
                          Editar
                        </Button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      {open !== null && (
        <CheckoutLinkModal link={open === 'new' ? null : open} onClose={() => setOpen(null)} onCopy={(token) => void copy(token)} />
      )}
    </Card>
  );
}

interface Draft {
  label: string;
  equipmentMode: 'sale' | 'loan';
  periodMonths: string;
  price: string;
  /** No incluso: a Farbo instala (no comodato, sempre). */
  farboInstall: boolean;
  install: string;
  uninstall: string;
  installBlocking: boolean;
  validUntil: string;
  maxUses: string;
  active: boolean;
}

function draftOf(l: CheckoutLink | null): Draft {
  if (!l) {
    return {
      label: '', equipmentMode: 'sale', periodMonths: '1', price: '', farboInstall: false, install: '', uninstall: '',
      installBlocking: false, validUntil: '', maxUses: '1', active: true,
    };
  }
  return {
    label: l.label,
    equipmentMode: l.equipmentMode,
    periodMonths: String(l.periodMonths),
    price: centsToInput(l.priceCents),
    farboInstall: l.farboInstall,
    install: l.installCents > 0 ? centsToInput(l.installCents) : '',
    uninstall: l.uninstallCents > 0 ? centsToInput(l.uninstallCents) : '',
    installBlocking: l.installBlocking,
    validUntil: l.validUntil ?? '',
    maxUses: l.maxUses !== null ? String(l.maxUses) : '',
    active: l.active,
  };
}

/** O rascunho como a API recebe; ou o problema, em texto. Vazio nas taxas é zero. */
export function checkoutLinkInput(d: Draft): CheckoutLinkInput | string {
  const label = d.label.trim();
  if (!label) return 'Dê um nome ao link: para quem ou para quê (só a central vê).';
  const priceCents = parseMoney(d.price);
  if (priceCents === null || priceCents < 100) return 'Valor inválido: use, por exemplo, 49,90 (a partir de R$ 1,00).';
  const loan = d.equipmentMode === 'loan';
  const installed = loan || d.farboInstall;
  const installCents = installed && d.install.trim() ? parseMoney(d.install) : 0;
  if (installCents === null) return 'Taxa de instalação inválida: use, por exemplo, 100,00 (vazio: sem custo).';
  const uninstallCents = loan && d.uninstall.trim() ? parseMoney(d.uninstall) : 0;
  if (uninstallCents === null) return 'Taxa de desinstalação inválida: use, por exemplo, 85,00 (vazio: sem custo).';
  let maxUses: number | null = null;
  if (d.maxUses.trim()) {
    maxUses = Number(d.maxUses);
    if (!Number.isInteger(maxUses) || maxUses < 1) return 'O limite de usos é um número a partir de 1 (vazio: sem limite).';
  }
  return {
    label,
    equipmentMode: d.equipmentMode,
    periodMonths: Number(d.periodMonths),
    priceCents,
    farboInstall: installed,
    installCents,
    uninstallCents,
    installBlocking: installed && d.installBlocking,
    validUntil: d.validUntil || null,
    maxUses,
    active: d.active,
  };
}

function CheckoutLinkModal({
  link,
  onClose,
  onCopy,
}: {
  link: CheckoutLink | null;
  onClose: () => void;
  onCopy: (token: string) => void;
}) {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [draft, setDraft] = useState<Draft>(() => draftOf(link));
  const [error, setError] = useState('');
  // O link recém-criado: o diálogo mostra o endereço para copiar.
  const [created, setCreated] = useState<CheckoutLink | null>(null);
  const set = <K extends keyof Draft>(field: K, value: Draft[K]) => setDraft((d) => ({ ...d, [field]: value }));
  const refresh = () => void queryClient.invalidateQueries({ queryKey: key });
  const save = useMutation({
    mutationFn: () => {
      const input = checkoutLinkInput(draft);
      if (typeof input === 'string') throw new Error(input);
      return link ? checkoutLinksApi.update(link.id, input) : checkoutLinksApi.create(input);
    },
    onSuccess: (saved) => {
      refresh();
      if (link) {
        notify({ tone: 'success', title: 'Link salvo', description: 'Vale para as contratações daqui em diante.' });
        onClose();
      } else {
        setCreated(saved);
      }
    },
    onError: (err: Error) => setError(err.message),
  });
  const remove = useMutation({
    mutationFn: () => checkoutLinksApi.remove((link as CheckoutLink).id),
    onSuccess: () => {
      refresh();
      notify({ tone: 'success', title: 'Link excluído' });
      onClose();
    },
    onError: (err: Error) => setError(err.message),
  });

  if (created) {
    return (
      <Modal
        open
        title="Link criado"
        onClose={onClose}
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              Fechar
            </Button>
            <Button variant="primary" onClick={() => onCopy(created.token)}>
              Copiar link
            </Button>
          </>
        }
      >
        <div className={pageStyles.form}>
          <p className={pageStyles.description}>
            <strong>{created.label}</strong>: {checkoutLinkOffer(created)}. Instalação: {checkoutLinkInstall(created).toLowerCase()}.
          </p>
          <TextField label="Endereço para mandar" readOnly value={checkoutLinkUrl(created.token)} onFocus={(e) => e.target.select()} />
          <p className={pageStyles.description}>
            Quem abrir vê só esta oferta, informa o veículo e os dados e paga com Pix.
            {created.maxUses === 1 ? ' Depois de uma contratação paga, o link deixa de valer.' : ''}
          </p>
        </div>
      </Modal>
    );
  }

  const loan = draft.equipmentMode === 'loan';
  const installed = loan || draft.farboInstall;
  const per = draft.periodMonths === '12' ? 'por ano' : draft.periodMonths === '6' ? 'por semestre' : 'por mês';
  return (
    <Modal
      open
      title={link ? `Link ${link.label}` : 'Novo link de pagamento'}
      onClose={onClose}
      footer={
        <>
          {link && link.uses === 0 && (
            <Button variant="danger" loading={remove.isPending} onClick={() => remove.mutate()}>
              Excluir
            </Button>
          )}
          <Button variant="ghost" onClick={onClose}>
            Cancelar
          </Button>
          <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()}>
            {link ? 'Salvar' : 'Criar link'}
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
        <TextField
          label="Para quem ou para quê (só a central vê)"
          maxLength={100}
          placeholder="Ex.: frota do João, feira de motos"
          value={draft.label}
          onChange={(e) => set('label', e.target.value)}
        />
        <div className={pageStyles.formRow}>
          <SelectField
            label="Rastreador"
            hint={loan ? 'Emprestado: volta à Farbo ao encerrar.' : 'Fica com o cliente.'}
            value={draft.equipmentMode}
            onChange={(e) => set('equipmentMode', e.target.value as Draft['equipmentMode'])}
          >
            <option value="sale">Incluso</option>
            <option value="loan">Comodato</option>
          </SelectField>
          <SelectField label="Recorrência" value={draft.periodMonths} onChange={(e) => set('periodMonths', e.target.value)}>
            <option value="1">Mensal</option>
            <option value="6">Semestral</option>
            <option value="12">Anual</option>
          </SelectField>
        </div>
        <TextField
          label={`Valor (R$ ${per})`}
          inputMode="decimal"
          placeholder={draft.periodMonths === '1' ? '49,90' : '600,00'}
          hint={
            loan
              ? 'Com tudo dentro: o comodato não soma a garantia de troca nem outra taxa por mês.'
              : draft.periodMonths === '1'
                ? 'Com o rastreador incluso dentro. Encerrado antes, a parte do rastreador que falta vence de uma vez, como no site.'
                : 'Com o rastreador incluso dentro, pago de uma vez a cada período.'
          }
          value={draft.price}
          onChange={(e) => set('price', e.target.value)}
        />
        <div className={pageStyles.formRow}>
          {!loan && (
            <SelectField
              label="Quem instala"
              value={draft.farboInstall ? 'farbo' : 'own'}
              onChange={(e) => set('farboInstall', e.target.value === 'farbo')}
            >
              <option value="own">O cliente, por conta própria</option>
              <option value="farbo">Os instaladores da Farbo</option>
            </SelectField>
          )}
          {installed && (
            <TextField
              label="Instalação (R$, opcional)"
              inputMode="decimal"
              placeholder="0,00"
              hint="Cobrada junto com a 1ª cobrança. Vazio: sem custo."
              value={draft.install}
              onChange={(e) => set('install', e.target.value)}
            />
          )}
        </div>
        <div className={pageStyles.formRow}>
          {installed && (
            <SelectField
              label="Bloqueio do motor"
              value={draft.installBlocking ? 'yes' : 'no'}
              onChange={(e) => set('installBlocking', e.target.value === 'yes')}
            >
              <option value="no">Sem bloqueio</option>
              <option value="yes">Com bloqueio (o relé vai na instalação)</option>
            </SelectField>
          )}
          {loan && (
            <TextField
              label="Desinstalação (R$, opcional)"
              inputMode="decimal"
              placeholder="0,00"
              hint="Cobrada ao encerrar. Vazio: sem custo."
              value={draft.uninstall}
              onChange={(e) => set('uninstall', e.target.value)}
            />
          )}
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
            hint={
              link
                ? `Já usado ${link.uses} ${link.uses === 1 ? 'vez' : 'vezes'}. 1: link único. Vazio: sem limite.`
                : '1: link único, para uma pessoa. Vazio: sem limite.'
            }
            value={draft.maxUses}
            onChange={(e) => set('maxUses', e.target.value.replace(/\D/g, '').slice(0, 6))}
          />
        </div>
        <label className={styles.promoOption}>
          <input type="checkbox" checked={draft.active} onChange={(e) => set('active', e.target.checked)} />
          <span>
            <strong>Valendo</strong>
            <br />
            Desligado, o link mostra que não vale mais. Quem já contratou segue nas condições que contratou, mesmo que você
            mude o link.
          </span>
        </label>
        {link && (
          <p className={pageStyles.description}>
            Endereço: {checkoutLinkUrl(link.token)}{' '}
            <Button size="small" variant="ghost" onClick={() => onCopy(link.token)}>
              Copiar
            </Button>
          </p>
        )}
      </div>
    </Modal>
  );
}

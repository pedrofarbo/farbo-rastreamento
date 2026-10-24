import { useEffect, useState } from 'react';

import { Button } from '@/components/ui/Button';
import { Field, TextField } from '@/components/ui/Field';
import fieldStyles from '@/components/ui/Field.module.css';
import { Modal } from '@/components/ui/Modal';
import { centsToInput, formatPhoneInput, parseMoney } from '@/services/format';
import { ORG_KIND_LABELS } from '@/services/roles';
import { formatTaxId, taxIdDigits, validTaxId } from '@/services/taxid';
import type { OrgKind, Organization, OrganizationInput } from '@/types';

import page from '@/pages/Page.module.css';
import styles from './Organization.module.css';

interface Draft {
  kind: OrgKind;
  name: string;
  document: string;
  email: string;
  phone: string;
  price: string;
  dueDay: string;
  equipmentMode: 'loan' | 'sale';
  equipment: string;
  installFee: string;
  uninstallFee: string;
  notes: string;
  active: boolean;
  adminName: string;
  adminEmail: string;
}

const EMPTY: Draft = {
  kind: 'association', name: '', document: '', email: '', phone: '', price: '', dueDay: '10',
  equipmentMode: 'loan', equipment: '', installFee: '', uninstallFee: '', notes: '', active: true,
  adminName: '', adminEmail: '',
};

function draftOf(o: Organization): Draft {
  return {
    kind: o.kind, name: o.name, document: formatTaxId(o.document), email: o.email, phone: o.phone,
    price: centsToInput(o.priceCents), dueDay: String(o.dueDay), equipmentMode: o.equipmentMode,
    equipment: o.equipmentCents ? centsToInput(o.equipmentCents) : '', installFee: o.installFeeCents ? centsToInput(o.installFeeCents) : '',
    uninstallFee: o.uninstallFeeCents ? centsToInput(o.uninstallFeeCents) : '', notes: o.notes, active: o.active,
    adminName: '', adminEmail: '',
  };
}

const EMAIL = /^[^\s@<>]+@[^\s@<>]+\.[^\s@<>]+$/;

/** O cadastro conferido, ou a mensagem do que falta. Vazio nas taxas é zero. */
export function organizationFromDraft(
  d: Draft,
  creating: boolean,
): { input: OrganizationInput; admin?: { name: string; email: string } } | string {
  const money = (text: string) => (text.trim() ? parseMoney(text) : 0);
  const price = money(d.price);
  const equipment = d.equipmentMode === 'sale' ? money(d.equipment) : 0;
  const install = money(d.installFee);
  const uninstall = money(d.uninstallFee);
  const dueDay = Number(d.dueDay);
  if (!d.name.trim()) return 'Informe o nome da organização.';
  if (d.document.trim() && !validTaxId(d.document)) return 'CNPJ (ou CPF) inválido: confira os números.';
  if (!EMAIL.test(d.email.trim())) return 'Informe o e-mail de cobrança: é para ele que vão as faturas e os lembretes.';
  if (price === null || !d.price.trim()) return 'Informe a mensalidade por veículo (zero, se não houver).';
  if (!Number.isInteger(dueDay) || dueDay < 1 || dueDay > 28) return 'O dia de vencimento vai de 1 a 28.';
  if (equipment === null || install === null || uninstall === null) return 'Confira os valores do rastreador e das taxas.';
  if (d.equipmentMode === 'sale' && !equipment) return 'Informe o preço do rastreador na venda.';
  const input: OrganizationInput = {
    kind: d.kind, name: d.name.trim(), document: taxIdDigits(d.document), email: d.email.trim(), phone: d.phone.trim(),
    priceCents: price, dueDay, equipmentMode: d.equipmentMode, equipmentCents: equipment, installFeeCents: install,
    uninstallFeeCents: uninstall, notes: d.notes.trim(),
    ...(creating ? {} : { active: d.active }),
  };
  if (creating && (d.adminName.trim() || d.adminEmail.trim())) {
    if (!d.adminName.trim() || !EMAIL.test(d.adminEmail.trim())) return 'Informe o nome e o e-mail do Administrador (ou deixe os dois vazios).';
    return { input, admin: { name: d.adminName.trim(), email: d.adminEmail.trim() } };
  }
  return { input };
}

/**
 * O cadastro da associação ou seguradora pela central: o contato de cobrança,
 * a mensalidade por veículo, o rastreador (comodato ou venda) e as taxas de
 * instalação e desinstalação. No cadastro novo, o primeiro Administrador
 * recebe o convite para o painel.
 */
export function OrganizationFormModal({
  open,
  organization,
  saving,
  onClose,
  onSave,
}: {
  open: boolean;
  organization: Organization | null;
  saving: boolean;
  onClose: () => void;
  onSave: (input: OrganizationInput, admin?: { name: string; email: string }) => void;
}) {
  const [draft, setDraft] = useState<Draft>(EMPTY);
  const [error, setError] = useState('');
  useEffect(() => {
    if (!open) return;
    setError('');
    setDraft(organization ? draftOf(organization) : EMPTY);
  }, [open, organization]);
  const creating = organization === null;
  const set = (patch: Partial<Draft>) => setDraft({ ...draft, ...patch });

  const submit = () => {
    const result = organizationFromDraft(draft, creating);
    if (typeof result === 'string') return setError(result);
    setError('');
    onSave(result.input, result.admin);
  };

  return (
    <Modal
      open={open}
      wide
      title={creating ? 'Nova associação ou seguradora' : `Editar ${organization?.name ?? ""}`}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancelar
          </Button>
          <Button variant="primary" loading={saving} onClick={submit}>
            {creating ? 'Cadastrar' : 'Salvar'}
          </Button>
        </>
      }
    >
      <div className={page.form}>
        {error && (
          <div className={styles.error} role="alert">
            {error}
          </div>
        )}
        <div className={styles.choices} role="radiogroup" aria-label="Tipo">
          {(Object.keys(ORG_KIND_LABELS) as OrgKind[]).map((kind) => (
            <label key={kind} className={`${styles.choice} ${draft.kind === kind ? styles.choiceActive : ''}`}>
              <input type="radio" name="org-kind" checked={draft.kind === kind} onChange={() => set({ kind })} />
              {ORG_KIND_LABELS[kind]}
            </label>
          ))}
        </div>
        <div className={page.formRow}>
          <TextField label="Nome" value={draft.name} maxLength={120} onChange={(e) => set({ name: e.target.value })} />
          <TextField
            label="CNPJ"
            inputMode="numeric"
            value={draft.document}
            onChange={(e) => set({ document: formatTaxId(e.target.value) })}
          />
        </div>
        <div className={page.formRow}>
          <TextField
            label="E-mail de cobrança"
            type="email"
            value={draft.email}
            hint="Recebe as faturas e os lembretes."
            onChange={(e) => set({ email: e.target.value })}
          />
          <TextField
            label="Telefone"
            inputMode="tel"
            value={draft.phone}
            onChange={(e) => set({ phone: formatPhoneInput(e.target.value, draft.phone) })}
          />
        </div>

        <div className={page.formRow}>
          <TextField
            label="Mensalidade por veículo (R$)"
            inputMode="decimal"
            placeholder="29,90"
            value={draft.price}
            hint="Por veículo com rastreador instalado, todo mês."
            onChange={(e) => set({ price: e.target.value })}
          />
          <TextField
            label="Dia de vencimento"
            type="number"
            min={1}
            max={28}
            value={draft.dueDay}
            onChange={(e) => set({ dueDay: e.target.value })}
          />
        </div>

        <Field label="Rastreador">
          {() => (
            <div className={styles.choices} role="radiogroup" aria-label="Rastreador">
              <label className={`${styles.choice} ${draft.equipmentMode === 'loan' ? styles.choiceActive : ''}`}>
                <input type="radio" name="org-equipment" checked={draft.equipmentMode === 'loan'} onChange={() => set({ equipmentMode: 'loan' })} />
                Comodato (volta na desinstalação)
              </label>
              <label className={`${styles.choice} ${draft.equipmentMode === 'sale' ? styles.choiceActive : ''}`}>
                <input type="radio" name="org-equipment" checked={draft.equipmentMode === 'sale'} onChange={() => set({ equipmentMode: 'sale' })} />
                Venda
              </label>
            </div>
          )}
        </Field>
        <div className={page.formRow}>
          {draft.equipmentMode === 'sale' && (
            <TextField
              label="Preço do rastreador (R$)"
              inputMode="decimal"
              value={draft.equipment}
              hint="Na fatura do mês da instalação."
              onChange={(e) => set({ equipment: e.target.value })}
            />
          )}
          <TextField
            label="Taxa de instalação (R$)"
            inputMode="decimal"
            placeholder="0,00"
            value={draft.installFee}
            onChange={(e) => set({ installFee: e.target.value })}
          />
          <TextField
            label="Taxa de desinstalação (R$)"
            inputMode="decimal"
            placeholder="0,00"
            value={draft.uninstallFee}
            onChange={(e) => set({ uninstallFee: e.target.value })}
          />
        </div>

        <Field label="Observações (só a central vê)">
          {(id) => (
            <textarea
              id={id}
              className={fieldStyles.input}
              rows={3}
              maxLength={1000}
              value={draft.notes}
              onChange={(e) => set({ notes: e.target.value })}
            />
          )}
        </Field>

        {creating ? (
          <>
            <p className={page.note}>
              O primeiro Administrador recebe o convite para o painel e põe o resto da equipe. Dá para cadastrar depois,
              na ficha da organização.
            </p>
            <div className={page.formRow}>
              <TextField label="Administrador: nome" value={draft.adminName} onChange={(e) => set({ adminName: e.target.value })} />
              <TextField
                label="Administrador: e-mail"
                type="email"
                value={draft.adminEmail}
                onChange={(e) => set({ adminEmail: e.target.value })}
              />
            </div>
          </>
        ) : (
          <label className={styles.choice} style={{ alignSelf: 'flex-start' }}>
            <input type="checkbox" checked={draft.active} onChange={(e) => set({ active: e.target.checked })} />
            Ativa (desativada, a equipe dela perde o painel na hora; os associados continuam com o app)
          </label>
        )}
      </div>
    </Modal>
  );
}

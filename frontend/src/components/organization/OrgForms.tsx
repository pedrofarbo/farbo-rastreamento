import { useEffect, useState } from 'react';

import type { VehicleInput } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import { SelectField, TextField } from '@/components/ui/Field';
import { Modal } from '@/components/ui/Modal';
import { EMPTY_VEHICLE, VehicleFields } from '@/components/vehicle/VehicleFields';
import { formatPhoneInput } from '@/services/format';
import { formatTaxId, taxIdDigits, validTaxId } from '@/services/taxid';
import type { Associate, AssociateInput } from '@/types';

import page from '@/pages/Page.module.css';
import styles from './Organization.module.css';

interface AssociateDraft {
  name: string;
  email: string;
  phone: string;
  document: string;
}

const EMPTY_ASSOCIATE: AssociateDraft = { name: '', email: '', phone: '', document: '' };

/** O que falta no cadastro do associado, ou null. Na edição, o e-mail não muda. */
export function associateProblem(d: AssociateDraft, editing: boolean): string | null {
  if (!d.name.trim()) return 'Informe o nome do associado.';
  if (!editing && !/^[^\s@<>]+@[^\s@<>]+\.[^\s@<>]+$/.test(d.email.trim())) {
    return 'Informe um e-mail válido: é por ele que o associado recebe o acesso ao app.';
  }
  if (d.document.trim() && !validTaxId(d.document)) return 'CPF (ou CNPJ) inválido: confira os números.';
  return null;
}

/**
 * O cadastro do associado (novo ou edição). O novo recebe por e-mail o
 * convite para criar a senha do app.
 */
export function AssociateModal({
  open,
  associate,
  saving,
  onClose,
  onSave,
  brokers,
}: {
  open: boolean;
  /** Nulo: associado novo. */
  associate: Associate | null;
  saving: boolean;
  onClose: () => void;
  onSave: (input: AssociateInput) => void;
  /** Os corretores da equipe (para o Administrador escolher); sem eles, o campo não aparece. */
  brokers?: { id: string; name: string }[];
}) {
  const [draft, setDraft] = useState<AssociateDraft>(EMPTY_ASSOCIATE);
  const [broker, setBroker] = useState('');
  const [error, setError] = useState('');
  useEffect(() => {
    if (!open) return;
    setError('');
    setBroker(associate?.brokerId ?? '');
    setDraft(
      associate
        ? { name: associate.name, email: associate.email, phone: associate.phone, document: formatTaxId(associate.document) }
        : EMPTY_ASSOCIATE,
    );
  }, [open, associate]);

  const submit = () => {
    const problem = associateProblem(draft, associate !== null);
    if (problem) return setError(problem);
    setError('');
    onSave({
      name: draft.name.trim(),
      ...(associate ? {} : { email: draft.email.trim() }),
      phone: draft.phone.trim(),
      document: taxIdDigits(draft.document),
      ...(brokers ? { brokerId: broker } : {}),
    });
  };

  return (
    <Modal
      open={open}
      title={associate ? `Editar ${associate.name}` : 'Novo associado'}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancelar
          </Button>
          <Button variant="primary" loading={saving} onClick={submit}>
            {associate ? 'Salvar' : 'Cadastrar e convidar'}
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
        <TextField label="Nome" value={draft.name} maxLength={120} onChange={(e) => setDraft({ ...draft, name: e.target.value })} />
        {!associate && (
          <TextField
            label="E-mail"
            type="email"
            value={draft.email}
            maxLength={254}
            hint="O associado recebe neste e-mail o convite para criar a senha do app."
            onChange={(e) => setDraft({ ...draft, email: e.target.value })}
          />
        )}
        <div className={page.formRow}>
          <TextField
            label="Celular"
            inputMode="tel"
            value={draft.phone}
            onChange={(e) => setDraft({ ...draft, phone: formatPhoneInput(e.target.value, draft.phone) })}
          />
          <TextField
            label="CPF ou CNPJ"
            inputMode="numeric"
            value={draft.document}
            onChange={(e) => setDraft({ ...draft, document: formatTaxId(e.target.value) })}
          />
        </div>
        {brokers && brokers.length > 0 && (
          <SelectField label="Corretor" value={broker} onChange={(e) => setBroker(e.target.value)}>
            <option value="">Nenhum</option>
            {brokers.map((b) => (
              <option key={b.id} value={b.id}>
                {b.name}
              </option>
            ))}
          </SelectField>
        )}
      </div>
    </Modal>
  );
}

/**
 * O veículo do associado: entra na fila de instalação da central e passa a
 * contar na fatura quando o rastreador é instalado.
 */
export function RegisterVehicleModal({
  open,
  ownerName,
  saving,
  onClose,
  onSave,
}: {
  open: boolean;
  ownerName: string;
  saving: boolean;
  onClose: () => void;
  onSave: (input: VehicleInput) => void;
}) {
  const [draft, setDraft] = useState<VehicleInput>(EMPTY_VEHICLE);
  const [error, setError] = useState('');
  useEffect(() => {
    if (open) {
      setDraft(EMPTY_VEHICLE);
      setError('');
    }
  }, [open]);
  const submit = () => {
    if (!draft.name.trim()) return setError('Informe o apelido do veículo.');
    setError('');
    onSave({ ...draft, name: draft.name.trim() });
  };
  return (
    <Modal
      open={open}
      title={`Novo veículo de ${ownerName}`}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancelar
          </Button>
          <Button variant="primary" loading={saving} onClick={submit}>
            Cadastrar e pedir a instalação
          </Button>
        </>
      }
    >
      <div className={page.form}>
        <p className={page.note}>
          O pedido de instalação vai para a central da Farbo, que combina a instalação do rastreador. O veículo entra na
          fatura da organização quando o rastreador é instalado.
        </p>
        {error && (
          <div className={styles.error} role="alert">
            {error}
          </div>
        )}
        <VehicleFields value={draft} onChange={setDraft} autoFocus />
      </div>
    </Modal>
  );
}

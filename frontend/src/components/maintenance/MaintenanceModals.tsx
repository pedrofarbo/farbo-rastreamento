import { useMemo, useState } from 'react';
import { useMutation } from '@tanstack/react-query';

import { maintenanceApi } from '@/api/resources';
import type { MaintenanceInput } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import { SelectField, TextField } from '@/components/ui/Field';
import { Modal } from '@/components/ui/Modal';
import { DOCUMENT_PRESETS, MAINTENANCE_SUGGESTIONS, brDate, parseWholeNumber, renewLabel } from '@/services/maintenance';
import { todayISO } from '@/services/format';
import type { MaintenanceItem, MaintenanceKind, VehicleGroup } from '@/types';

import pageStyles from '@/pages/Page.module.css';
import styles from './Maintenance.module.css';

/** Um veículo para escolher (o da frota). */
export interface PickVehicle {
  id: string;
  name: string;
  plate?: string;
  groupId?: string | null;
}

type Scope = 'all' | 'group' | 'pick';

/** O formulário: os números como texto (a pessoa digita "10.000"). */
interface Draft {
  kind: MaintenanceKind;
  name: string;
  /** Documento: o próximo vencimento. */
  dueDate: string;
  everyKm: string;
  everyMonths: string;
  everyHours: string;
  lastDate: string;
  lastKm: string;
  lastHours: string;
}

/** O pedido a partir do formulário, ou a mensagem do que falta. */
export function maintenanceInput(d: Draft, single: boolean): MaintenanceInput | string {
  const name = d.name.trim();
  if (d.kind === 'document') {
    if (!name) return 'Dê um nome ao documento (por exemplo, IPVA).';
    const months = parseWholeNumber(d.everyMonths);
    if (months === null || Number.isNaN(months) || months < 1 || months > 120)
      return 'Informe de quantos em quantos meses o documento renova (12 para o IPVA, 60 para a CNH).';
    if (!d.dueDate) return 'Informe a data do próximo vencimento.';
    return { kind: 'document', name, everyKm: null, everyMonths: months, everyHours: null, dueDate: d.dueDate };
  }
  if (!name) return 'Dê um nome à manutenção (por exemplo, Troca de óleo).';
  const everyKm = parseWholeNumber(d.everyKm);
  const everyMonths = parseWholeNumber(d.everyMonths);
  const everyHours = parseWholeNumber(d.everyHours);
  if (Number.isNaN(everyKm) || Number.isNaN(everyMonths) || Number.isNaN(everyHours)) return 'Use só números nos intervalos.';
  if (everyKm === null && everyMonths === null && everyHours === null)
    return 'Informe a cada quantos km, horas de motor ou meses (dá para combinar).';
  const lastKm = single ? parseWholeNumber(d.lastKm) : null;
  if (Number.isNaN(lastKm)) return 'O km da última vez precisa ser um número.';
  const lastHours = single && everyHours !== null ? parseWholeNumber(d.lastHours) : null;
  if (Number.isNaN(lastHours)) return 'As horas da última vez precisam ser um número.';
  return { name, everyKm, everyMonths, everyHours, lastDate: d.lastDate, lastKm, lastHours };
}

/** Um número guardado (km, horas) para o campo do formulário. */
const asText = (n: number | null | undefined) => (n != null ? String(Math.round(n)) : '');

/**
 * Criar ou editar uma manutenção. Na frota, vale para todos os veículos, um
 * grupo ou os escolhidos; num veículo só, dá para informar o km e as horas
 * de motor da última vez (sem eles, o hodômetro e o horímetro de agora).
 */
export function MaintenanceFormModal({
  open,
  onClose,
  onSaved,
  item,
  vehicles,
  groups = [],
}: {
  open: boolean;
  onClose: () => void;
  onSaved: (message: string) => void;
  /** Editando esta (senão, criando). */
  item?: MaintenanceItem | null;
  /** Os veículos que podem receber: um só (o da tela do veículo) ou a frota. */
  vehicles: PickVehicle[];
  groups?: VehicleGroup[];
}) {
  const single = vehicles.length === 1 || Boolean(item);
  const [draft, setDraft] = useState<Draft>(() => ({
    kind: item?.kind ?? 'service',
    dueDate: item?.kind === 'document' ? (item.nextDate ?? '') : '',
    name: item?.name ?? '',
    everyKm: asText(item?.everyKm),
    everyMonths: asText(item?.everyMonths),
    everyHours: asText(item?.everyHours),
    lastDate: item?.lastDate ?? todayISO(),
    lastKm: asText(item?.lastKm),
    lastHours: asText(item?.lastHours),
  }));
  const [scope, setScope] = useState<Scope>('all');
  const [group, setGroup] = useState(groups[0]?.id ?? '');
  const [picked, setPicked] = useState<string[]>([]);
  const [search, setSearch] = useState('');
  const [error, setError] = useState('');

  const chosen = useMemo(() => {
    if (single) return vehicles.map((v) => v.id);
    if (scope === 'all') return vehicles.map((v) => v.id);
    if (scope === 'group') return vehicles.filter((v) => v.groupId === group).map((v) => v.id);
    return picked;
  }, [single, scope, group, picked, vehicles]);

  const shown = useMemo(() => {
    const q = search.trim().toLowerCase();
    return q ? vehicles.filter((v) => `${v.name} ${v.plate ?? ''}`.toLowerCase().includes(q)) : vehicles;
  }, [search, vehicles]);

  const document = draft.kind === 'document';
  // Manutenção é feminino ("criada"); documento, masculino ("criado").
  const done = document ? 'o' : 'a';
  const save = useMutation({
    mutationFn: async (input: MaintenanceInput) => {
      if (item) {
        await maintenanceApi.update(item.id, input);
        return `${input.name} atualizad${done}`;
      }
      const { created } = await maintenanceApi.create({ ...input, vehicleIds: chosen });
      return created === 1 ? `${input.name} criad${done}` : `${input.name} criad${done} em ${created} veículos`;
    },
    onSuccess: (message) => onSaved(message),
    onError: (err: Error) => setError(err.message),
  });

  const submit = () => {
    const input = maintenanceInput(draft, single);
    if (typeof input === 'string') return setError(input);
    if (chosen.length === 0) return setError('Escolha ao menos um veículo.');
    setError('');
    save.mutate(input);
  };

  return (
    <Modal
      open={open}
      title={item ? (document ? 'Editar documento' : 'Editar manutenção') : document ? 'Novo documento' : 'Nova manutenção'}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancelar
          </Button>
          <Button variant="primary" loading={save.isPending} onClick={submit}>
            {item ? 'Salvar' : chosen.length > 1 ? `Criar em ${chosen.length} veículos` : 'Criar'}
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
        {!item && (
          <div className={styles.choices} role="radiogroup" aria-label="O que cadastrar">
            {(
              [
                ['service', 'Manutenção'],
                ['document', 'Documento (IPVA, CNH...)'],
              ] as [MaintenanceKind, string][]
            ).map(([value, label]) => (
              <label key={value} className={`${styles.choice} ${draft.kind === value ? styles.choiceOn : ''}`}>
                <input
                  type="radio"
                  name="maintenance-kind"
                  checked={draft.kind === value}
                  onChange={() => setDraft({ ...draft, kind: value, name: '', everyMonths: value === 'document' ? '12' : '' })}
                />
                {label}
              </label>
            ))}
          </div>
        )}
        {document ? (
          <DocumentFields draft={draft} setDraft={setDraft} />
        ) : (
          <>
        <TextField
          label="O que fazer"
          list="maintenance-suggestions"
          maxLength={60}
          value={draft.name}
          placeholder="Troca de óleo"
          onChange={(e) => setDraft({ ...draft, name: e.target.value })}
        />
        <datalist id="maintenance-suggestions">
          {MAINTENANCE_SUGGESTIONS.map((s) => (
            <option key={s} value={s} />
          ))}
        </datalist>
        <div className={pageStyles.formRow}>
          <TextField
            label="A cada (km)"
            inputMode="numeric"
            value={draft.everyKm}
            placeholder="10.000"
            onChange={(e) => setDraft({ ...draft, everyKm: e.target.value })}
          />
          <TextField
            label="Ou a cada (horas de motor)"
            inputMode="numeric"
            value={draft.everyHours}
            placeholder="250"
            onChange={(e) => setDraft({ ...draft, everyHours: e.target.value })}
          />
          <TextField
            label="Ou a cada (meses)"
            inputMode="numeric"
            value={draft.everyMonths}
            placeholder="6"
            onChange={(e) => setDraft({ ...draft, everyMonths: e.target.value })}
          />
        </div>
        <p className={styles.muted}>
          O que vier primeiro. Deixe em branco o que não se aplica: o licenciamento é só por data; caminhões, máquinas e
          geradores costumam ir por horas de motor (andando ou parado com a ignição ligada).
        </p>
        <div className={pageStyles.formRow}>
          <TextField
            label="Feita pela última vez em"
            type="date"
            max={todayISO()}
            value={draft.lastDate}
            onChange={(e) => setDraft({ ...draft, lastDate: e.target.value })}
          />
          {single && (
            <TextField
              label="Com quantos km"
              inputMode="numeric"
              value={draft.lastKm}
              placeholder="o hodômetro de agora"
              onChange={(e) => setDraft({ ...draft, lastKm: e.target.value })}
            />
          )}
          {single && draft.everyHours.trim() !== '' && (
            <TextField
              label="Com quantas horas de motor"
              inputMode="numeric"
              value={draft.lastHours}
              placeholder="o horímetro de agora"
              onChange={(e) => setDraft({ ...draft, lastHours: e.target.value })}
            />
          )}
        </div>
        {!single && (
          <p className={styles.muted}>Cada veículo começa a contar pelo hodômetro e pelo horímetro dele de agora.</p>
        )}
          </>
        )}

        {!single && (
          <fieldset className={styles.scope}>
            <legend>Em quais veículos</legend>
            <div className={styles.choices} role="radiogroup" aria-label="Em quais veículos">
              {(
                [
                  ['all', `Toda a frota (${vehicles.length})`],
                  ...(groups.length > 0 ? [['group', 'Um grupo']] : []),
                  ['pick', 'Escolher'],
                ] as [Scope, string][]
              ).map(([value, label]) => (
                <label key={value} className={`${styles.choice} ${scope === value ? styles.choiceOn : ''}`}>
                  <input type="radio" name="maintenance-scope" checked={scope === value} onChange={() => setScope(value)} />
                  {label}
                </label>
              ))}
            </div>
            {scope === 'group' && (
              <SelectField label="Grupo" value={group} onChange={(e) => setGroup(e.target.value)}>
                {groups.map((g) => (
                  <option key={g.id} value={g.id}>
                    {g.name}
                  </option>
                ))}
              </SelectField>
            )}
            {scope === 'pick' && (
              <>
                <TextField label="Buscar" value={search} placeholder="Nome ou placa" onChange={(e) => setSearch(e.target.value)} />
                <ul className={styles.pickList}>
                  {shown.slice(0, 200).map((v) => (
                    <li key={v.id}>
                      <label>
                        <input
                          type="checkbox"
                          checked={picked.includes(v.id)}
                          onChange={(e) =>
                            setPicked(e.target.checked ? [...picked, v.id] : picked.filter((id) => id !== v.id))
                          }
                        />
                        {v.name}
                        {v.plate && <span className={styles.plate}>{v.plate}</span>}
                      </label>
                    </li>
                  ))}
                </ul>
                {shown.length > 200 && <p className={styles.muted}>Busque para achar os outros {shown.length - 200}.</p>}
              </>
            )}
            <p className={styles.muted}>
              {chosen.length === 1 ? '1 veículo escolhido.' : `${chosen.length} veículos escolhidos.`}
            </p>
          </fieldset>
        )}
      </div>
    </Modal>
  );
}

/** Os campos de um documento: o nome (com os mais comuns), a renovação e o próximo vencimento. */
function DocumentFields({ draft, setDraft }: { draft: Draft; setDraft: (d: Draft) => void }) {
  const months = parseWholeNumber(draft.everyMonths);
  return (
    <>
      <TextField
        label="Documento"
        list="document-suggestions"
        maxLength={60}
        value={draft.name}
        placeholder="IPVA"
        onChange={(e) => {
          const name = e.target.value;
          const preset = DOCUMENT_PRESETS.find((p) => p.name === name);
          setDraft({ ...draft, name, everyMonths: preset ? String(preset.months) : draft.everyMonths });
        }}
      />
      <datalist id="document-suggestions">
        {DOCUMENT_PRESETS.map((p) => (
          <option key={p.name} value={p.name} />
        ))}
      </datalist>
      <div className={pageStyles.formRow}>
        <TextField
          label="Próximo vencimento"
          type="date"
          value={draft.dueDate}
          onChange={(e) => setDraft({ ...draft, dueDate: e.target.value })}
        />
        <TextField
          label="Renova a cada (meses)"
          inputMode="numeric"
          value={draft.everyMonths}
          placeholder="12"
          onChange={(e) => setDraft({ ...draft, everyMonths: e.target.value })}
        />
      </div>
      <p className={styles.muted}>
        {draft.dueDate ? `Vence em ${brDate(draft.dueDate)}. ` : ''}
        {months && !Number.isNaN(months) ? `${renewLabel(months)}. ` : ''}
        Avisamos 30 dias antes e no dia. Ao registrar a renovação, o vencimento passa para o seguinte, no mesmo dia do
        calendário.
      </p>
    </>
  );
}

/**
 * Registrar manutenções feitas: a data, o km e as horas de motor (com uma
 * só; com várias, o hodômetro e o horímetro de cada veículo) e uma
 * observação. A contagem recomeça daí.
 */
export function DoneModal({
  items,
  onClose,
  onDone,
}: {
  items: MaintenanceItem[];
  onClose: () => void;
  onDone: (message: string) => void;
}) {
  const single = items.length === 1;
  // Documento: renovado (sem km nem horas).
  const document = items.length > 0 && items.every((i) => i.kind === 'document');
  // As horas só pedem campo quando a manutenção vence por elas.
  const withHours = single && !document && items[0].everyHours !== null;
  const [date, setDate] = useState(todayISO());
  const [km, setKm] = useState('');
  const [hours, setHours] = useState('');
  const [notes, setNotes] = useState('');
  const [error, setError] = useState('');
  const done = useMutation({
    mutationFn: () => {
      const parsed = single && !document ? parseWholeNumber(km) : null;
      if (Number.isNaN(parsed)) throw new Error('O km precisa ser um número.');
      const parsedHours = withHours ? parseWholeNumber(hours) : null;
      if (Number.isNaN(parsedHours)) throw new Error('As horas de motor precisam ser um número.');
      return maintenanceApi.done({ itemIds: items.map((i) => i.id), date, km: parsed, hours: parsedHours, notes });
    },
    onSuccess: ({ done: n }) =>
      onDone(
        document
          ? n === 1
            ? `${items[0].name} renovado`
            : `${n} documentos renovados`
          : n === 1
            ? `${items[0].name} registrada`
            : `${n} manutenções registradas`,
      ),
    onError: (err: Error) => setError(err.message),
  });

  return (
    <Modal
      open={items.length > 0}
      title={
        document
          ? single
            ? `Renovar: ${items[0]?.name}`
            : `Renovar ${items.length} documentos`
          : single
            ? `Registrar: ${items[0]?.name}`
            : `Registrar ${items.length} manutenções`
      }
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancelar
          </Button>
          <Button variant="primary" loading={done.isPending} onClick={() => done.mutate()}>
            Registrar
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
        <p className={pageStyles.description}>
          {document
            ? `${single ? `${items[0]?.vehicleName}: o` : 'O'} vencimento passa para o seguinte, no mesmo dia do calendário.`
            : !single
            ? 'A contagem de cada uma recomeça desta data e do hodômetro e do horímetro de cada veículo.'
            : withHours
              ? `${items[0]?.vehicleName}: a contagem recomeça desta data, deste km e destas horas de motor.`
              : `${items[0]?.vehicleName}: a contagem recomeça desta data e deste km.`}
        </p>
        <div className={pageStyles.formRow}>
          <TextField
            label={document ? 'Renovado em' : 'Feita em'}
            type="date"
            max={todayISO()}
            value={date}
            onChange={(e) => setDate(e.target.value)}
          />
          {single && !document && (
            <TextField
              label="Com quantos km"
              inputMode="numeric"
              value={km}
              placeholder="o hodômetro de agora"
              onChange={(e) => setKm(e.target.value)}
            />
          )}
          {withHours && (
            <TextField
              label="Com quantas horas de motor"
              inputMode="numeric"
              value={hours}
              placeholder="o horímetro de agora"
              onChange={(e) => setHours(e.target.value)}
            />
          )}
        </div>
        <TextField
          label="Observação (opcional)"
          maxLength={300}
          value={notes}
          placeholder="Oficina, peças, valor…"
          onChange={(e) => setNotes(e.target.value)}
        />
      </div>
    </Modal>
  );
}

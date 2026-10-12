import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { maintenanceApi } from '@/api/resources';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { TextField } from '@/components/ui/Field';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatDateOnly } from '@/services/format';
import { STATUS_LABEL, formatHours, formatKm, intervalLabel, parseWholeNumber, remainingLabel } from '@/services/maintenance';
import type { MaintenanceItem, MaintenanceStatus } from '@/types';

import { DoneModal, MaintenanceFormModal } from './MaintenanceModals';
import styles from './Maintenance.module.css';

export const STATUS_TONE: Record<MaintenanceStatus, 'danger' | 'warning' | 'success' | 'neutral'> = {
  due: 'danger',
  soon: 'warning',
  ok: 'success',
  unknown: 'neutral',
};

export const vehicleMaintenanceKey = (vehicleId: string) => ['vehicle', vehicleId, 'maintenance'] as const;

/**
 * Um medidor do veículo (o hodômetro ou o horímetro): o valor, de onde vem
 * e, para quem pode, informar ou corrigir a leitura.
 */
function Meter({
  label,
  value,
  hint,
  editLabel,
  initial,
  canEdit,
  informed,
  saving,
  onSave,
}: {
  label: string;
  value: string;
  hint: string;
  editLabel: string;
  /** O valor que o campo traz ao abrir. */
  initial: string;
  canEdit: boolean;
  /** Já tem leitura informada (o botão diz Corrigir). */
  informed: boolean;
  saving: boolean;
  /** Salva a leitura; true fecha o campo. */
  onSave: (text: string) => Promise<boolean>;
}) {
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState('');
  return (
    <div className={styles.odometer}>
      {editing ? (
        <div className={styles.odometerEdit}>
          <TextField label={editLabel} inputMode="numeric" autoFocus value={text} onChange={(e) => setText(e.target.value)} />
          <div className={styles.rowActions}>
            <Button size="small" variant="primary" loading={saving} onClick={async () => (await onSave(text)) && setEditing(false)}>
              Salvar
            </Button>
            <Button size="small" variant="ghost" onClick={() => setEditing(false)}>
              Cancelar
            </Button>
          </div>
        </div>
      ) : (
        <>
          <div>
            <span className={styles.label}>{label}</span>
            <strong className={styles.odometerValue}>{value}</strong>
            <span className={styles.muted}>{hint}</span>
          </div>
          {canEdit && (
            <Button
              size="small"
              variant="ghost"
              onClick={() => {
                setText(initial);
                setEditing(true);
              }}
            >
              {informed ? 'Corrigir' : 'Informar'}
            </Button>
          )}
        </>
      )}
    </div>
  );
}

/**
 * O hodômetro, o horímetro e a manutenção de um veículo: as leituras
 * (informar ou corrigir), o que vence e quanto falta, e o histórico. O
 * horímetro só aparece quando alguma manutenção vence por horas de motor (ou
 * já foi informado). Quem só acompanha vê; quem mexe na conta registra e
 * edita.
 */
export function VehicleMaintenance({
  vehicle,
  canEdit,
  canEditOdometer = canEdit,
}: {
  vehicle: { id: string; name: string; plate?: string };
  /** Cadastrar, editar e registrar manutenções (o titular e o Gestor). */
  canEdit: boolean;
  /** Informar o hodômetro (também o admin da central). */
  canEditOdometer?: boolean;
}) {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const key = vehicleMaintenanceKey(vehicle.id);
  const data = useQuery({ queryKey: key, queryFn: () => maintenanceApi.vehicle(vehicle.id) });
  const [form, setForm] = useState<{ item: MaintenanceItem | null } | null>(null);
  const [doing, setDoing] = useState<MaintenanceItem[]>([]);
  const [allRecords, setAllRecords] = useState(false);
  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: key });
    queryClient.invalidateQueries({ queryKey: ['me', 'maintenance'] });
  };

  const setOdometer = useMutation({
    mutationFn: (km: number) => maintenanceApi.setOdometer(vehicle.id, km),
    onSuccess: (o) => {
      refresh();
      notify({ tone: 'success', title: `Hodômetro: ${formatKm(o.km ?? 0)}`, description: 'Daqui em diante, somam-se os km medidos pelo GPS.' });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Hodômetro não salvo', description: err.message }),
  });
  const setEngineHours = useMutation({
    mutationFn: (hours: number) => maintenanceApi.setEngineHours(vehicle.id, hours),
    onSuccess: (o) => {
      refresh();
      notify({ tone: 'success', title: `Horímetro: ${formatHours(o.hours)}`, description: 'Daqui em diante, somam-se as horas de motor medidas.' });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Horímetro não salvo', description: err.message }),
  });
  const remove = useMutation({
    mutationFn: (item: MaintenanceItem) => maintenanceApi.remove(item.id),
    onSuccess: (_, item) => {
      refresh();
      notify({ tone: 'success', title: `${item.name} apagada`, description: 'O histórico do que já foi feito continua.' });
    },
  });

  const odometer = data.data?.odometer;
  const items = data.data?.items ?? [];
  const records = data.data?.records ?? [];
  const showHours = Boolean(odometer?.hoursSetAt) || items.some((it) => it.everyHours !== null);
  // Salva a leitura digitada; false deixa o campo aberto (número inválido ou erro).
  const saveReading = async (text: string, invalid: string, mutation: typeof setOdometer) => {
    const n = parseWholeNumber(text);
    if (n === null || Number.isNaN(n)) {
      notify({ tone: 'error', title: invalid });
      return false;
    }
    return mutation.mutateAsync(n).then(
      () => true,
      () => false,
    );
  };

  return (
    <Card
      title="Hodômetro e manutenção"
      actions={
        canEdit ? (
          <Button size="small" variant="secondary" onClick={() => setForm({ item: null })}>
            Nova manutenção
          </Button>
        ) : undefined
      }
    >
      {data.isLoading ? (
        <Spinner inline />
      ) : (
        <div className={styles.vehicle}>
          <div className={styles.meters}>
            <Meter
              label="Hodômetro"
              value={odometer?.km != null ? formatKm(odometer.km) : 'não informado'}
              hint={
                odometer?.km != null
                  ? `Leitura de ${formatDateOnly(odometer.setAt?.slice(0, 10))} mais os km do GPS.`
                  : `O GPS mediu ${formatKm(odometer?.trackedKm ?? 0)} desde a instalação. Informe a leitura do painel para contar as manutenções por km.`
              }
              editLabel="Hodômetro do veículo agora (km)"
              initial={odometer?.km != null ? String(Math.round(odometer.km)) : ''}
              canEdit={canEditOdometer}
              informed={odometer?.km != null}
              saving={setOdometer.isPending}
              onSave={(text) => saveReading(text, 'Informe o hodômetro em km', setOdometer)}
            />
            {showHours && (
              <Meter
                label="Horímetro"
                value={formatHours(odometer?.hours ?? 0)}
                hint={
                  odometer?.hoursSetAt
                    ? `Leitura de ${formatDateOnly(odometer.hoursSetAt.slice(0, 10))} mais as horas de motor medidas.`
                    : 'Horas de motor medidas pelo rastreador desde a instalação (andando ou parado com a ignição ligada). Se o veículo tem horímetro, informe a leitura.'
                }
                editLabel="Horímetro do veículo agora (horas)"
                initial={String(Math.round(odometer?.hours ?? 0))}
                canEdit={canEditOdometer}
                informed={Boolean(odometer?.hoursSetAt)}
                saving={setEngineHours.isPending}
                onSave={(text) => saveReading(text, 'Informe o horímetro em horas', setEngineHours)}
              />
            )}
          </div>

          {items.length === 0 ? (
            <p className={styles.muted}>Nenhuma manutenção cadastrada para este veículo.</p>
          ) : (
            <ul className={styles.items}>
              {items.map((item) => (
                <li key={item.id} className={styles.item}>
                  <div className={styles.itemHead}>
                    <strong>{item.name}</strong>
                    <Badge tone={STATUS_TONE[item.status]}>{STATUS_LABEL[item.status]}</Badge>
                  </div>
                  <span className={styles.muted}>
                    {intervalLabel(item)} · última em {formatDateOnly(item.lastDate)}
                    {item.lastKm !== null && ` com ${formatKm(item.lastKm)}`}
                    {item.everyHours !== null && item.lastHours !== null && `${item.lastKm !== null ? ' e' : ' com'} ${formatHours(item.lastHours)}`}
                  </span>
                  <span className={item.status === 'due' ? styles.due : item.status === 'soon' ? styles.soon : styles.muted}>
                    {remainingLabel(item)}
                  </span>
                  {canEdit && (
                    <div className={styles.rowActions}>
                      <Button size="small" variant="secondary" onClick={() => setDoing([item])}>
                        Registrar feita
                      </Button>
                      <Button size="small" variant="ghost" onClick={() => setForm({ item })}>
                        Editar
                      </Button>
                      <Button size="small" variant="ghost" loading={remove.isPending && remove.variables?.id === item.id} onClick={() => remove.mutate(item)}>
                        Apagar
                      </Button>
                    </div>
                  )}
                </li>
              ))}
            </ul>
          )}

          {records.length > 0 && (
            <div>
              <span className={styles.label}>Feitas</span>
              <ul className={styles.records}>
                {(allRecords ? records : records.slice(0, 5)).map((r) => (
                  <li key={r.id}>
                    <span>{formatDateOnly(r.doneDate)}</span>
                    <span>
                      <strong>{r.name}</strong>
                      {r.km !== null && ` · ${formatKm(r.km)}`}
                      {showHours && r.hours !== null && ` · ${formatHours(r.hours)}`}
                      {r.notes && <span className={styles.muted}> · {r.notes}</span>}
                    </span>
                  </li>
                ))}
              </ul>
              {records.length > 5 && !allRecords && (
                <Button size="small" variant="ghost" onClick={() => setAllRecords(true)}>
                  Ver todas ({records.length})
                </Button>
              )}
            </div>
          )}
        </div>
      )}

      {form && (
        <MaintenanceFormModal
          open
          item={form.item}
          vehicles={[vehicle]}
          onClose={() => setForm(null)}
          onSaved={(message) => {
            setForm(null);
            refresh();
            notify({ tone: 'success', title: message });
          }}
        />
      )}
      {doing.length > 0 && (
        <DoneModal
          items={doing}
          onClose={() => setDoing([])}
          onDone={(message) => {
            setDoing([]);
            refresh();
            notify({ tone: 'success', title: message });
          }}
        />
      )}
    </Card>
  );
}

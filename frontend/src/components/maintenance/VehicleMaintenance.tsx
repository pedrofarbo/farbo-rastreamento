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
import { STATUS_LABEL, formatKm, intervalLabel, parseWholeNumber, remainingLabel } from '@/services/maintenance';
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
 * O hodômetro e a manutenção de um veículo: a leitura (informar ou
 * corrigir), o que vence e quanto falta, e o histórico. Quem só acompanha vê;
 * quem mexe na conta registra e edita.
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
  const [editingOdometer, setEditingOdometer] = useState(false);
  const [odometerText, setOdometerText] = useState('');
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
      setEditingOdometer(false);
      notify({ tone: 'success', title: `Hodômetro: ${formatKm(o.km ?? 0)}`, description: 'Daqui em diante, somam-se os km medidos pelo GPS.' });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Hodômetro não salvo', description: err.message }),
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
  const saveOdometer = () => {
    const km = parseWholeNumber(odometerText);
    if (km === null || Number.isNaN(km)) return notify({ tone: 'error', title: 'Informe o hodômetro em km' });
    setOdometer.mutate(km);
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
          <div className={styles.odometer}>
            {editingOdometer ? (
              <div className={styles.odometerEdit}>
                <TextField
                  label="Hodômetro do veículo agora (km)"
                  inputMode="numeric"
                  autoFocus
                  value={odometerText}
                  onChange={(e) => setOdometerText(e.target.value)}
                />
                <div className={styles.rowActions}>
                  <Button size="small" variant="primary" loading={setOdometer.isPending} onClick={saveOdometer}>
                    Salvar
                  </Button>
                  <Button size="small" variant="ghost" onClick={() => setEditingOdometer(false)}>
                    Cancelar
                  </Button>
                </div>
              </div>
            ) : (
              <>
                <div>
                  <span className={styles.label}>Hodômetro</span>
                  <strong className={styles.odometerValue}>{odometer?.km != null ? formatKm(odometer.km) : 'não informado'}</strong>
                  <span className={styles.muted}>
                    {odometer?.km != null
                      ? `Leitura de ${formatDateOnly(odometer.setAt?.slice(0, 10))} mais os km do GPS.`
                      : `O GPS mediu ${formatKm(odometer?.trackedKm ?? 0)} desde a instalação. Informe a leitura do painel para contar as manutenções por km.`}
                  </span>
                </div>
                {canEditOdometer && (
                  <Button
                    size="small"
                    variant="ghost"
                    onClick={() => {
                      setOdometerText(odometer?.km != null ? String(Math.round(odometer.km)) : '');
                      setEditingOdometer(true);
                    }}
                  >
                    {odometer?.km != null ? 'Corrigir' : 'Informar'}
                  </Button>
                )}
              </>
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

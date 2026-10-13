import { useMemo, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { smsSetupApi } from '@/api/resources';
import type { BulkSmsSetupResponse } from '@/api/resources';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Modal } from '@/components/ui/Modal';
import { useToast } from '@/components/ui/Toast';
import { SMS_BULK_LIMIT, chunk, failedBatch, isDeviceActivated, mergeBulkResults } from '@/services/deviceBulk';
import { formatDeviceStatus, formatPhoneInput } from '@/services/format';
import type { Device, Vehicle } from '@/types';

import styles from './DeviceBulkActivationModal.module.css';

interface DeviceBulkActivationModalProps {
  open: boolean;
  onClose: () => void;
  devices: Device[];
  vehicles: Vehicle[];
  initialSelectedIds?: string[];
  onSuccess?: () => void;
}

export function DeviceBulkActivationModal({
  open,
  onClose,
  devices,
  vehicles,
  initialSelectedIds = [],
  onSuccess,
}: DeviceBulkActivationModalProps) {
  const { notify } = useToast();
  const queryClient = useQueryClient();

  const [search, setSearch] = useState('');
  const [unlock, setUnlock] = useState(false);
  const [ask, setAsk] = useState(false);
  const [hideActivated, setHideActivated] = useState(true);
  const [allowReactivation, setAllowReactivation] = useState(false);
  const [results, setResults] = useState<BulkSmsSetupResponse | null>(null);
  // Com mais de SMS_BULK_LIMIT, os lotes saem um atrás do outro: quantos já foram.
  const [progress, setProgress] = useState<{ done: number; total: number } | null>(null);
  const [submitError, setSubmitError] = useState('');

  // Map of vehicle by deviceId for quick lookup
  const vehicleByDeviceId = useMemo(() => {
    const map = new Map<string, Vehicle>();
    for (const v of vehicles) {
      if (v.deviceId) map.set(v.deviceId, v);
    }
    return map;
  }, [vehicles]);

  // Contagem de aparelhos já ativados vs pendentes
  const activatedDevices = useMemo(
    () => devices.filter((d) => isDeviceActivated(d)),
    [devices],
  );

  const pendingDevicesWithChip = useMemo(
    () =>
      devices.filter(
        (d) => !isDeviceActivated(d) && Boolean(d.phoneNumber && d.phoneNumber.trim().length > 0),
      ),
    [devices],
  );

  // Set of selected device IDs
  const [selectedIds, setSelectedIds] = useState<Set<string>>(() => {
    const initial = new Set<string>();
    const initialSet = new Set(initialSelectedIds);
    for (const d of devices) {
      if (d.phoneNumber && (initialSet.size === 0 || initialSet.has(d.id))) {
        // Se fornecido IDs iniciais, só seleciona os que não foram ativados (a menos que seja seleção explícita)
        if (initialSet.size > 0 && initialSet.has(d.id)) {
          initial.add(d.id);
        }
      }
    }
    return initial;
  });

  // Trackers with phone number
  const eligibleDevices = useMemo(
    () => devices.filter((d) => Boolean(d.phoneNumber && d.phoneNumber.trim().length > 0)),
    [devices],
  );

  // Filtered devices according to search and hideActivated
  const filteredDevices = useMemo(() => {
    let list = devices;
    if (hideActivated) {
      list = list.filter((d) => !isDeviceActivated(d));
    }
    const q = search.trim().toLowerCase();
    if (!q) return list;
    return list.filter((d) => {
      const v = vehicleByDeviceId.get(d.id);
      return (
        d.imei.toLowerCase().includes(q) ||
        (d.phoneNumber && d.phoneNumber.toLowerCase().includes(q)) ||
        (d.model && d.model.toLowerCase().includes(q)) ||
        (v && v.name.toLowerCase().includes(q)) ||
        (v && v.plate && v.plate.toLowerCase().includes(q))
      );
    });
  }, [devices, hideActivated, search, vehicleByDeviceId]);

  const toggleSelect = (id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  };

  const selectPendingWithChip = () => {
    const all = new Set<string>();
    for (const d of filteredDevices) {
      if (d.phoneNumber && d.phoneNumber.trim().length > 0 && (!isDeviceActivated(d) || allowReactivation)) {
        all.add(d.id);
      }
    }
    setSelectedIds(all);
  };

  const deselectAll = () => {
    setSelectedIds(new Set());
  };

  // O servidor aceita até SMS_BULK_LIMIT por vez: acima disso, a tela manda em
  // lotes, um atrás do outro, e junta os resultados. Um lote que falhar inteiro
  // (rede, sessão) entra no resultado com o erro, e os outros seguem.
  const startBulk = useMutation({
    mutationFn: async () => {
      const ids = Array.from(selectedIds);
      const imeiOf = (id: string) => devices.find((d) => d.id === id)?.imei;
      const parts: BulkSmsSetupResponse[] = [];
      let done = 0;
      try {
        for (const batch of chunk(ids, SMS_BULK_LIMIT)) {
          setProgress({ done, total: ids.length });
          try {
            parts.push(await smsSetupApi.startBulk({ deviceIds: batch, unlock, query: ask, force: allowReactivation }));
          } catch (err) {
            parts.push(failedBatch(batch, err instanceof Error ? err.message : 'Falha ao enviar o lote.', imeiOf));
          }
          done += batch.length;
        }
      } finally {
        setProgress(null);
      }
      return mergeBulkResults(parts);
    },
    onSuccess: (data) => {
      setResults(data);
      setSubmitError('');
      queryClient.invalidateQueries({ queryKey: ['devices'] });
      queryClient.invalidateQueries({ queryKey: ['sms-setup'] });
      queryClient.invalidateQueries({ queryKey: ['sms-usage'] });
      if (data.started > 0) {
        notify({
          tone: 'success',
          title: 'Ativação em massa iniciada',
          description: `${data.started} rastreador(es) iniciaram o envio de comandos por SMS.`,
        });
      }
      if (data.failed > 0) {
        notify({
          tone: 'warning',
          title: 'Alguns rastreadores não foram ativados',
          description: `${data.failed} rastreador(es) falharam ou já estavam ativos.`,
        });
      }
    },
    onError: (err: Error) => {
      setSubmitError(err.message || 'Falha ao iniciar ativação em massa.');
    },
  });

  const handleClose = () => {
    if (results) {
      onSuccess?.();
    }
    onClose();
  };

  if (!open) return null;

  return (
    <Modal
      open={open}
      wide
      title={results ? 'Resultado da ativação em massa' : 'Ativação de rastreadores em massa'}
      onClose={handleClose}
      footer={
        results ? (
          <Button variant="primary" onClick={handleClose}>
            Concluir
          </Button>
        ) : (
          <>
            <Button variant="ghost" onClick={handleClose}>
              Cancelar
            </Button>
            <Button
              variant="primary"
              disabled={selectedIds.size === 0}
              loading={startBulk.isPending}
              onClick={() => startBulk.mutate()}
            >
              {progress
                ? `Enviando ${Math.min(progress.done + SMS_BULK_LIMIT, progress.total)} de ${progress.total}…`
                : `Ativar ${selectedIds.size > 0 ? `${selectedIds.size} rastreador(es)` : ''}`}
            </Button>
          </>
        )
      }
    >
      <div className={styles.container}>
        {!results ? (
          <>
            <p className={styles.intro}>
              Selecione os rastreadores que deseja ativar. O sistema enviará os comandos SMS de
              configuração (APN, Servidor Farbo, Fuso e Frequência) para o chip de cada um.
            </p>

            <div className={styles.filterBar}>
              <input
                type="text"
                className={styles.searchInput}
                placeholder="Buscar por IMEI, número do chip, placa ou veículo..."
                value={search}
                onChange={(e) => setSearch(e.target.value)}
              />
              <div className={styles.quickButtons}>
                <Button size="small" variant="ghost" onClick={selectPendingWithChip}>
                  Selecionar pendentes ({pendingDevicesWithChip.length})
                </Button>
                {selectedIds.size > 0 && (
                  <Button size="small" variant="ghost" onClick={deselectAll}>
                    Desmarcar todos
                  </Button>
                )}
              </div>
            </div>

            <div className={styles.statsBar}>
              <div className={styles.statPills}>
                <span className={`${styles.statPill} ${styles.statSelected}`}>
                  {selectedIds.size} selecionado(s) para ativação
                </span>
                <span className={styles.statPill}>
                  {pendingDevicesWithChip.length} pendente(s) com chip
                </span>
                {activatedDevices.length > 0 && (
                  <span className={styles.statPill} style={{ color: 'var(--success)' }}>
                    {activatedDevices.length} já ativado(s)
                  </span>
                )}
                {devices.length - eligibleDevices.length > 0 && (
                  <span className={`${styles.statPill} ${styles.statWarning}`}>
                    {devices.length - eligibleDevices.length} sem número de chip
                  </span>
                )}
              </div>
              <label
                style={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  gap: 'var(--space-2)',
                  fontSize: 'var(--text-xs)',
                  cursor: 'pointer',
                  userSelect: 'none',
                  fontWeight: 500,
                  color: 'var(--text-secondary)',
                }}
              >
                <input
                  type="checkbox"
                  checked={hideActivated}
                  onChange={(e) => setHideActivated(e.target.checked)}
                />
                Ocultar já ativados ({activatedDevices.length})
              </label>
            </div>

            <div className={styles.tableWrap}>
              <table className={styles.table}>
                <thead>
                  <tr>
                    <th style={{ width: 36, textAlign: 'center' }}>
                      <input
                        type="checkbox"
                        checked={
                          filteredDevices.length > 0 &&
                          filteredDevices
                            .filter((d) => d.phoneNumber && (!isDeviceActivated(d) || allowReactivation))
                            .every((d) => selectedIds.has(d.id))
                        }
                        onChange={(e) => {
                          if (e.target.checked) selectPendingWithChip();
                          else deselectAll();
                        }}
                      />
                    </th>
                    <th>IMEI</th>
                    <th>Número do chip</th>
                    <th>Veículo</th>
                    <th>Situação</th>
                    <th>Ativação</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredDevices.map((device) => {
                    const hasPhone = Boolean(device.phoneNumber && device.phoneNumber.trim().length > 0);
                    const isActivated = isDeviceActivated(device);
                    const canSelect = hasPhone && (!isActivated || allowReactivation);
                    const isSelected = selectedIds.has(device.id);
                    const vehicle = vehicleByDeviceId.get(device.id);

                    return (
                      <tr
                        key={device.id}
                        className={!canSelect ? styles.rowDisabled : undefined}
                        onClick={() => {
                          if (canSelect) toggleSelect(device.id);
                        }}
                        style={{ cursor: canSelect ? 'pointer' : 'default' }}
                      >
                        <td style={{ textAlign: 'center' }} onClick={(e) => e.stopPropagation()}>
                          <input
                            type="checkbox"
                            checked={isSelected}
                            disabled={!canSelect}
                            onChange={() => toggleSelect(device.id)}
                          />
                        </td>
                        <td className={styles.mono}>
                          <strong>{device.imei}</strong>
                          {device.model && (
                            <span style={{ color: 'var(--text-muted)', marginLeft: 6 }}>
                              ({device.model})
                            </span>
                          )}
                        </td>
                        <td className={styles.mono}>
                          {hasPhone ? (
                            formatPhoneInput(device.phoneNumber!)
                          ) : (
                            <span className={styles.dangerText}>Sem número de chip</span>
                          )}
                        </td>
                        <td>
                          {vehicle ? (
                            <span>
                              {vehicle.name}{' '}
                              {vehicle.plate && (
                                <span className={styles.mono}>({vehicle.plate})</span>
                              )}
                            </span>
                          ) : (
                            <span style={{ color: 'var(--text-muted)' }}>— sem vínculo —</span>
                          )}
                        </td>
                        <td>
                          <Badge
                            tone={
                              device.status === 'ONLINE'
                                ? 'success'
                                : device.status === 'STALE'
                                  ? 'warning'
                                  : 'neutral'
                            }
                            dot
                          >
                            {formatDeviceStatus(device.status)}
                          </Badge>
                        </td>
                        <td>
                          {isActivated ? (
                            <Badge tone="success">Já ativado</Badge>
                          ) : hasPhone ? (
                            <Badge tone="neutral">Pendente</Badge>
                          ) : (
                            <span className={styles.dangerText}>Sem chip</span>
                          )}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>

            <div className={styles.optionsBox}>
              <div className={styles.optionsTitle}>Opções de envio por SMS:</div>
              <label>
                <input
                  type="checkbox"
                  checked={unlock}
                  onChange={(e) => setUnlock(e.target.checked)}
                />
                Destravar o canal de comandos antes (CMDLOCK,123456,0#)
              </label>
              <label>
                <input
                  type="checkbox"
                  checked={ask}
                  onChange={(e) => setAsk(e.target.checked)}
                />
                Pedir a configuração de volta no fim (PARAM#)
              </label>
              {!hideActivated && activatedDevices.length > 0 && (
                <label style={{ color: 'var(--warning)', marginTop: 4 }}>
                  <input
                    type="checkbox"
                    checked={allowReactivation}
                    onChange={(e) => setAllowReactivation(e.target.checked)}
                  />
                  <strong>Permitir reativar rastreadores já ativados ({activatedDevices.length})</strong>
                </label>
              )}
            </div>

            {submitError && (
              <div className={styles.dangerText} role="alert">
                {submitError}
              </div>
            )}
          </>
        ) : (
          <div className={styles.resultCard}>
            <div
              className={`${styles.summaryBox} ${
                results.failed === 0 ? styles.summarySuccess : styles.summaryMixed
              }`}
            >
              <div>
                <strong>
                  {results.started} de {results.total} ativações iniciadas com sucesso!
                </strong>
                {results.failed > 0 && (
                  <div style={{ color: 'var(--danger)', marginTop: 4 }}>
                    {results.failed} rastreador(es) falharam ao iniciar o envio de SMS.
                  </div>
                )}
              </div>
            </div>

            <div className={styles.resultList}>
              <table className={styles.table}>
                <thead>
                  <tr>
                    <th>IMEI</th>
                    <th>Status</th>
                    <th>Detalhes</th>
                  </tr>
                </thead>
                <tbody>
                  {results.results.map((res) => (
                    <tr key={res.deviceId}>
                      <td className={styles.mono}>
                        <strong>{res.imei || res.deviceId}</strong>
                      </td>
                      <td>
                        <Badge tone={res.success ? 'success' : 'danger'}>
                          {res.success ? 'Iniciado' : 'Erro'}
                        </Badge>
                      </td>
                      <td>
                        {res.success ? (
                          <span style={{ color: 'var(--text-muted)' }}>
                            Comandos SMS enviados para a fila da SMS Market
                          </span>
                        ) : (
                          <span className={styles.dangerText}>{res.error || 'Erro desconhecido'}</span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}
      </div>
    </Modal>
  );
}

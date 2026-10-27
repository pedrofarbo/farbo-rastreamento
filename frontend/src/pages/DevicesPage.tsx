import { useMemo, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router-dom';

import { chipsApi, customersApi, deviceInputFrom, devicesApi, diagnosticsApi, vehiclesApi } from '@/api/resources';
import type { DeviceInput, VehicleInput } from '@/api/resources';
import billing from '@/components/billing/Billing.module.css';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { DeviceActivationModal } from '@/components/devices/DeviceActivationModal';
import { DeviceBulkActivationModal } from '@/components/devices/DeviceBulkActivationModal';
import { DeviceBulkModal } from '@/components/devices/DeviceBulkModal';
import { UnknownDevicesCard } from '@/components/devices/UnknownDevicesCard';
import { EmptyState } from '@/components/ui/EmptyState';
import { fieldStyles, SelectField, TextField } from '@/components/ui/Field';
import { Modal } from '@/components/ui/Modal';
import { Pager } from '@/components/ui/Pager';
import { SmsCounter } from '@/components/fulfillment/SmsCounter';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { useOverflowX } from '@/hooks/useOverflowX';
import { usePagedList } from '@/hooks/usePagination';
import { isDeviceActivated } from '@/services/deviceBulk';
import { imeiProblem } from '@/services/imei';
import { formatDeviceStatus, formatICCID, formatPhoneInput, formatRelative } from '@/services/format';
import { useAuth } from '@/stores/AuthContext';
import type { Device } from '@/types';

import styles from './Page.module.css';

const EMPTY_DEVICE: DeviceInput = {
  imei: '',
  model: '',
  manufacturer: 'TKSTAR',
  protocol: '',
  phoneNumber: '',
  iccid: '',
  apn: '',
  apnUser: '',
  apnPassword: '',
  serverHost: '',
  serverPort: null,
  commandPassword: '',
};

/**
 * Campo de senha só de escrita: a API nunca devolve a senha, só diz se ela
 * está definida. Em branco mantém a atual; "Apagar" remove.
 */
function SecretField({
  label,
  hint,
  isSet,
  value,
  clear,
  onChange,
  onClear,
}: {
  label: string;
  hint: string;
  isSet: boolean;
  value: string;
  clear: boolean;
  onChange: (value: string) => void;
  onClear: (clear: boolean) => void;
}) {
  return (
    <div>
      <TextField
        label={label}
        type="password"
        autoComplete="new-password"
        placeholder={isSet && !clear ? '•••••• (definida)' : ''}
        disabled={clear}
        hint={
          !isSet ? hint : clear ? 'Será apagada ao salvar.' : 'Definida — deixe em branco para manter.'
        }
        value={value}
        onChange={(event) => onChange(event.target.value)}
      />
      {isSet && (
        <label style={{ display: 'flex', gap: 'var(--space-2)', alignItems: 'center' }}>
          <input type="checkbox" checked={clear} onChange={(event) => onClear(event.target.checked)} />
          Apagar a senha
        </label>
      )}
    </div>
  );
}

export function DevicesPage() {
  const { canOperate } = useAuth();
  const { notify } = useToast();
  const queryClient = useQueryClient();

  const [deviceForm, setDeviceForm] = useState<DeviceInput | null>(null);
  // Rastreador em edição: é dele que vêm os indicadores de senha definida.
  const [editing, setEditing] = useState<Device | null>(null);
  const editingId = editing?.id ?? null;
  const [provisioningFor, setProvisioningFor] = useState<Device | null>(null);
  const [vehicleForm, setVehicleForm] = useState<VehicleInput | null>(null);
  const [bulkOpen, setBulkOpen] = useState(false);
  const [activatingDevice, setActivatingDevice] = useState<Device | null>(null);
  const [bulkActivationOpen, setBulkActivationOpen] = useState(false);
  const [selectedDeviceIds, setSelectedDeviceIds] = useState<Set<string>>(new Set());
  const [activationFilter, setActivationFilter] = useState<'all' | 'pending' | 'activated'>('all');

  const devices = useQuery({ queryKey: ['devices'], queryFn: devicesApi.list });
  const vehicles = useQuery({ queryKey: ['vehicles'], queryFn: vehiclesApi.list });
  const customers = useQuery({ queryKey: ['customers'], queryFn: customersApi.list });
  const protocols = useQuery({ queryKey: ['protocols'], queryFn: diagnosticsApi.protocols });
  const chips = useQuery({ queryKey: ['chips'], queryFn: chipsApi.list });

  const provisioning = useQuery({
    queryKey: ['provisioning', provisioningFor?.id],
    queryFn: () => devicesApi.provisioning(provisioningFor!.id),
    enabled: Boolean(provisioningFor),
  });

  const saveDevice = useMutation({
    mutationFn: (input: DeviceInput) =>
      editingId ? devicesApi.update(editingId, input) : devicesApi.create(input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['devices'] });
      queryClient.invalidateQueries({ queryKey: ['chips'] });
      notify({ tone: 'success', title: editingId ? 'Rastreador atualizado' : 'Rastreador cadastrado' });
      setDeviceForm(null);
      setEditing(null);
    },
    onError: (error: Error) =>
      notify({ tone: 'error', title: 'Não foi possível salvar', description: error.message }),
  });

  const saveVehicle = useMutation({
    mutationFn: (input: VehicleInput) => vehiclesApi.create(input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['vehicles'] });
      notify({ tone: 'success', title: 'Veículo cadastrado' });
      setVehicleForm(null);
    },
    onError: (error: Error) =>
      notify({ tone: 'error', title: 'Não foi possível salvar', description: error.message }),
  });

  const vehicleByDevice = new Map(
    (vehicles.data ?? [])
      .filter((vehicle) => vehicle.deviceId)
      .map((vehicle) => [vehicle.deviceId as string, vehicle]),
  );

  const customerById = new Map(
    (customers.data ?? []).map((customer) => [customer.id, customer]),
  );

  const allDevices = devices.data ?? [];
  const activatedCount = allDevices.filter(isDeviceActivated).length;
  const pendingCount = allDevices.length - activatedCount;

  // O chip do estoque: o escolhido no formulário ou, sem escolha, o que o
  // rastreador em edição já tem. Os livres (e o dele) podem ser escolhidos.
  const chipList = chips.data ?? [];
  const selectableChips = chipList.filter((c) => !c.device || c.device.id === editingId);
  const selectedChip =
    deviceForm?.chipId !== undefined
      ? (chipList.find((c) => c.id === deviceForm.chipId) ?? null)
      : (chipList.find((c) => c.iccid !== '' && c.iccid === deviceForm?.iccid) ?? null);

  // IMEI fora do padrão: o aviso aparece já no formulário e o salvar pede a
  // conferência na etiqueta (com ele errado, a configuração por SMS se perde).
  const imeiIssue = deviceForm ? imeiProblem(deviceForm.imei) : '';
  const imeiBlocked = Boolean(imeiIssue) && !deviceForm?.imeiConfirmed;

  const displayedDevices = useMemo(() => {
    if (activationFilter === 'pending') return allDevices.filter((d) => !isDeviceActivated(d));
    if (activationFilter === 'activated') return allDevices.filter(isDeviceActivated);
    return allDevices;
  }, [allDevices, activationFilter]);
  const paged = usePagedList(displayedDevices, 'devices');
  const pageIds = paged.items.map((d) => d.id);
  const filterBy = (filter: typeof activationFilter) => {
    setActivationFilter(filter);
    paged.setPage(1);
  };
  // Trocando de página, a lista volta ao começo.
  const top = useRef<HTMLDivElement>(null);
  const goTo = (page: number) => {
    paged.setPage(page);
    top.current?.scrollIntoView?.({ block: 'start' });
  };
  const [tableWrap, tableOverflowing] = useOverflowX<HTMLDivElement>();

  return (
    <div className={styles.page}>
      <div className={`${styles.inner} ${styles.wide}`}>
        <header className={styles.header}>
          <div>
            <h1 className={styles.title}>Rastreadores</h1>
            <p className={styles.description}>
              Cadastre o IMEI antes de apontar o aparelho para o servidor: tráfego de IMEI
              desconhecido é recusado. O protocolo é preenchido sozinho no primeiro pacote.
            </p>
          </div>
          <div style={{ display: 'flex', gap: 'var(--space-2)', flexWrap: 'wrap' }}>
            <Button onClick={() => setVehicleForm({ name: '' })}>Veículo da central</Button>
            <Button onClick={() => setBulkOpen(true)}>Importar em massa</Button>
            <Button onClick={() => setBulkActivationOpen(true)}>Ativação em massa</Button>
            <Button
              variant="primary"
              onClick={() => {
                setEditing(null);
                setDeviceForm(EMPTY_DEVICE);
              }}
            >
              Novo rastreador
            </Button>
          </div>
        </header>

        {canOperate && <SmsCounter />}

        <UnknownDevicesCard
          devices={allDevices}
          onRegister={(imei) => {
            setEditing(null);
            setDeviceForm({ ...EMPTY_DEVICE, imei });
          }}
        />

        {allDevices.length > 0 && (
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              gap: 'var(--space-3)',
              flexWrap: 'wrap',
              marginBottom: 'var(--space-3)',
            }}
          >
            <div style={{ display: 'flex', gap: 'var(--space-2)' }}>
              <Button
                size="small"
                variant={activationFilter === 'all' ? 'primary' : 'ghost'}
                onClick={() => filterBy('all')}
              >
                Todos ({allDevices.length})
              </Button>
              <Button
                size="small"
                variant={activationFilter === 'pending' ? 'primary' : 'ghost'}
                onClick={() => filterBy('pending')}
              >
                Pendentes de ativação ({pendingCount})
              </Button>
              <Button
                size="small"
                variant={activationFilter === 'activated' ? 'primary' : 'ghost'}
                onClick={() => filterBy('activated')}
              >
                Já ativados ({activatedCount})
              </Button>
            </div>
          </div>
        )}

        {selectedDeviceIds.size > 0 && (
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              gap: 'var(--space-3)',
              padding: 'var(--space-2) var(--space-4)',
              background: 'var(--surface-sunken)',
              border: '1px solid var(--accent)',
              borderRadius: 'var(--radius-md)',
              marginBottom: 'var(--space-3)',
            }}
          >
            <div style={{ fontSize: 'var(--text-sm)', color: 'var(--text-primary)' }}>
              <strong>{selectedDeviceIds.size}</strong> rastreador(es) selecionado(s)
            </div>
            <div style={{ display: 'flex', gap: 'var(--space-2)' }}>
              <Button
                size="small"
                variant="primary"
                onClick={() => setBulkActivationOpen(true)}
              >
                Ativar selecionados ({selectedDeviceIds.size})
              </Button>
              <Button
                size="small"
                variant="ghost"
                onClick={() => setSelectedDeviceIds(new Set())}
              >
                Limpar seleção
              </Button>
            </div>
          </div>
        )}

        <div ref={top} className={styles.anchor}>
          <Card flush>
            {devices.isLoading ? (
              <Spinner label="Carregando rastreadores" />
            ) : (devices.data ?? []).length === 0 ? (
              <EmptyState
                icon="📡"
                title="Nenhum rastreador cadastrado"
                description="Cadastre o IMEI impresso no aparelho para que ele possa se conectar."
              />
            ) : (
              <div ref={tableWrap} className={`${styles.tableWrap} ${tableOverflowing ? styles.overflowing : ''}`}>
                <table className={`${styles.table} ${styles.dense}`}>
                  <thead>
                    <tr>
                      <th style={{ width: 36, textAlign: 'center' }}>
                        {/* Marca (ou desmarca) os desta página; os das outras ficam como estão. */}
                        <input
                          type="checkbox"
                          aria-label="Selecionar os rastreadores desta página"
                          checked={pageIds.length > 0 && pageIds.every((id) => selectedDeviceIds.has(id))}
                          onChange={(e) => {
                            const checked = e.target.checked;
                            setSelectedDeviceIds((prev) => {
                              const next = new Set(prev);
                              for (const id of pageIds) {
                                if (checked) next.add(id);
                                else next.delete(id);
                              }
                              return next;
                            });
                          }}
                        />
                      </th>
                      <th>IMEI</th>
                      <th>Veículo</th>
                      <th>Placa</th>
                      <th>Cliente</th>
                      <th>Número</th>
                      <th>Protocolo</th>
                      <th>Situação</th>
                      <th style={{ whiteSpace: 'normal' }}>Último contato</th>
                      <th className={styles.stickyEnd} />
                    </tr>
                  </thead>
                  <tbody>
                    {paged.items.map((device) => {
                      const vehicle = vehicleByDevice.get(device.id);
                      const customer = vehicle?.ownerId ? customerById.get(vehicle.ownerId) : null;
                      return (
                        <tr key={device.id}>
                          <td style={{ textAlign: 'center' }}>
                            <input
                              type="checkbox"
                              aria-label={`Selecionar ${device.imei}`}
                              checked={selectedDeviceIds.has(device.id)}
                              onChange={() => {
                                setSelectedDeviceIds((prev) => {
                                  const next = new Set(prev);
                                  if (next.has(device.id)) next.delete(device.id);
                                  else next.add(device.id);
                                  return next;
                                });
                              }}
                            />
                          </td>
                          <td className={styles.mono}>
                            <span style={{ whiteSpace: 'nowrap' }}>{device.imei}</span>
                            {/* O ICCID quebra nos grupos de 4 para a coluna não alargar. */}
                            {device.iccid && (
                              <div className={billing.muted} title="ICCID do chip" style={{ wordBreak: 'normal' }}>
                                chip {formatICCID(device.iccid)}
                              </div>
                            )}
                          </td>
                          <td style={{ minWidth: 140 }}>
                            {vehicle ? (
                              <Link
                                to={`/veiculos/${vehicle.id}`}
                                style={{ color: 'var(--text-primary)', textDecoration: 'none', fontWeight: 500 }}
                              >
                                {vehicle.name}
                              </Link>
                            ) : (
                              <span className={billing.muted}>— sem vínculo —</span>
                            )}
                          </td>
                          <td className={styles.mono} style={{ whiteSpace: 'nowrap' }}>
                            {vehicle?.plate ? (
                              vehicle.plate
                            ) : (
                              <span className={billing.muted}>—</span>
                            )}
                          </td>
                          <td style={{ minWidth: 140 }}>
                            {customer ? (
                              <Link
                                to={`/clientes/${customer.id}`}
                                style={{ color: 'var(--accent)', textDecoration: 'none' }}
                                title={`Ver cliente ${customer.name}`}
                              >
                                {customer.name}
                              </Link>
                            ) : vehicle ? (
                              <span className={billing.muted}>Central</span>
                            ) : (
                              <span className={billing.muted}>—</span>
                            )}
                          </td>
                          <td className={styles.mono} style={{ whiteSpace: 'nowrap' }}>
                            {device.phoneNumber ? (
                              formatPhoneInput(device.phoneNumber)
                            ) : (
                              <span className={billing.muted}>—</span>
                            )}
                          </td>
                          <td className={styles.mono}>{device.protocol || 'aguardando'}</td>
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
                        <td>{formatRelative(device.lastSeenAt)}</td>
                        <td className={styles.stickyEnd}>
                          {/* Os botões numa linha só: as outras colunas é que quebram. */}
                          <div className={styles.actions} style={{ flexWrap: 'nowrap' }}>
                            {isDeviceActivated(device) ? (
                              <Button
                                size="small"
                                variant="ghost"
                                onClick={() => setActivatingDevice(device)}
                                title="Rastreador já ativo e transmitindo. Clique para reconfigurar via SMS."
                                style={{ opacity: 0.8 }}
                              >
                                Reativar
                              </Button>
                            ) : (
                              <Button
                                size="small"
                                variant="primary"
                                onClick={() => setActivatingDevice(device)}
                                title="Ativar rastreador via comandos SMS"
                              >
                                Ativar
                              </Button>
                            )}
                            <Button
                              size="small"
                              variant="ghost"
                              onClick={() => setProvisioningFor(device)}
                            >
                              Configuração
                            </Button>
                            <Button
                              size="small"
                              variant="ghost"
                              onClick={() => {
                                setEditing(device);
                                setDeviceForm(deviceInputFrom(device));
                              }}
                            >
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
            {displayedDevices.length > 0 && (
              <Pager
                page={paged.page}
                pageSize={paged.pageSize}
                total={paged.total}
                onPage={goTo}
                onPageSize={paged.setPageSize}
              />
            )}
          </Card>
        </div>
      </div>

      {/* Cadastro / edição de rastreador */}
      <Modal
        open={deviceForm !== null}
        wide
        title={editingId ? 'Editar rastreador' : 'Novo rastreador'}
        onClose={() => setDeviceForm(null)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setDeviceForm(null)}>
              Cancelar
            </Button>
            <Button
              variant="primary"
              loading={saveDevice.isPending}
              disabled={imeiBlocked}
              title={imeiBlocked ? 'Confira o IMEI na etiqueta antes de salvar.' : undefined}
              onClick={() => deviceForm && saveDevice.mutate({ ...deviceForm, chipId: selectedChip?.id ?? null })}
            >
              Salvar
            </Button>
          </>
        }
      >
        {deviceForm && (
          <div className={styles.form}>
            <div className={styles.formRow}>
              <TextField
                label="IMEI"
                value={deviceForm.imei}
                hint="15 dígitos, impresso no aparelho."
                error={
                  imeiIssue
                    ? `Este IMEI ${imeiIssue}. Confira na etiqueta: com o IMEI errado, o rastreador conecta e é recusado, e os SMS da ativação se perdem.`
                    : undefined
                }
                onChange={(event) => setDeviceForm({ ...deviceForm, imei: event.target.value, imeiConfirmed: false })}
              />
              <TextField
                label="Modelo"
                placeholder="TK910"
                value={deviceForm.model ?? ''}
                onChange={(event) => setDeviceForm({ ...deviceForm, model: event.target.value })}
              />
            </div>

            {imeiIssue && (
              <label style={{ display: 'flex', gap: 'var(--space-2)', alignItems: 'center' }}>
                <input
                  type="checkbox"
                  checked={Boolean(deviceForm.imeiConfirmed)}
                  onChange={(event) => setDeviceForm({ ...deviceForm, imeiConfirmed: event.target.checked })}
                />
                Conferi na etiqueta: o IMEI está certo assim mesmo
              </label>
            )}

            <SelectField
              label="Protocolo"
              hint="Deixe em branco para o servidor detectar no primeiro pacote."
              value={deviceForm.protocol ?? ''}
              onChange={(event) =>
                setDeviceForm({ ...deviceForm, protocol: event.target.value })
              }
            >
              <option value="">detectar automaticamente</option>
              {(protocols.data ?? []).map((protocol) => (
                <option key={protocol.name} value={protocol.name}>
                  {protocol.label} — {protocol.confidence}
                </option>
              ))}
            </SelectField>

            <SelectField
              label="Chip"
              hint={
                selectedChip
                  ? 'A linha, o ICCID e o APN vêm do estoque. Para corrigir, edite o chip em Chips.'
                  : selectableChips.length === 0
                    ? 'Nenhum chip livre no estoque: digite os dados abaixo (o chip entra no estoque) ou cadastre em Chips.'
                    : 'Escolha um chip livre do estoque, ou digite os dados abaixo (o chip entra no estoque).'
              }
              value={selectedChip?.id ?? ''}
              onChange={(event) => {
                const chip = chipList.find((c) => c.id === event.target.value);
                setDeviceForm(
                  chip
                    ? {
                        ...deviceForm,
                        chipId: chip.id,
                        phoneNumber: chip.phoneNumber,
                        iccid: chip.iccid,
                        apn: chip.apn,
                        apnUser: chip.apnUser,
                        apnPassword: '',
                        clearApnPassword: false,
                      }
                    : { ...deviceForm, chipId: null, phoneNumber: '', iccid: '', apn: '', apnUser: '', apnPassword: '' },
                );
              }}
            >
              <option value="">Digitar os dados do chip</option>
              {selectableChips.map((c) => (
                <option key={c.id} value={c.id}>
                  {formatICCID(c.iccid)}
                  {c.phoneNumber ? ` · ${formatPhoneInput(c.phoneNumber)}` : ''}
                  {c.carrier ? ` · ${c.carrier}` : ''}
                </option>
              ))}
            </SelectField>

            {selectedChip && (
              <p className={styles.note}>
                Linha {selectedChip.phoneNumber ? formatPhoneInput(selectedChip.phoneNumber) : 'não cadastrada'} · ICCID{' '}
                {formatICCID(selectedChip.iccid)}
                {selectedChip.apn
                  ? ` · APN ${selectedChip.apn}${selectedChip.apnUser ? ` (${selectedChip.apnUser}${selectedChip.apnPasswordSet ? ', com senha' : ''})` : ''}`
                  : ' · APN padrão da central'}
              </p>
            )}

            {!selectedChip && (
              <div className={styles.formRow}>
                <TextField
                  label="Linha do chip"
                  placeholder="(34) 99999-0000"
                  hint="O número de telefone do chip, com DDD: é para ele que vai a configuração por SMS."
                  value={deviceForm.phoneNumber ?? ''}
                  onChange={(event) =>
                    setDeviceForm({
                      ...deviceForm,
                      phoneNumber: formatPhoneInput(event.target.value, deviceForm.phoneNumber ?? ''),
                    })
                  }
                />
                <TextField
                  label="ICCID do chip"
                  placeholder="89553202100093795330"
                  inputMode="numeric"
                  hint="Os 19 ou 20 números impressos no chip (começam com 89). Se colar com o SP da etiqueta, ele sai."
                  value={deviceForm.iccid ?? ''}
                  onChange={(event) => setDeviceForm({ ...deviceForm, iccid: event.target.value })}
                />
              </div>
            )}

            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                gap: 'var(--space-3)',
                flexWrap: 'wrap',
                padding: 'var(--space-2) var(--space-3)',
                background: 'var(--surface-sunken)',
                border: '1px dashed var(--border-subtle, var(--accent))',
                borderRadius: 'var(--radius-md)',
              }}
            >
              <span style={{ fontSize: 'var(--text-sm)', color: 'var(--text-secondary)' }}>
                {selectedChip
                  ? 'Servidor Farbo (tcp.farborastreadores.com.br:5000); o APN vem do chip'
                  : 'Servidor Farbo + chip Algar (tcp.farborastreadores.com.br:5000, APN ss.algar.br)'}
              </span>
              <Button
                size="small"
                variant="primary"
                id="device-autofill-farbo-algar"
                onClick={() =>
                  setDeviceForm({
                    ...deviceForm,
                    serverHost: 'tcp.farborastreadores.com.br',
                    serverPort: 5000,
                    apn: 'ss.algar.br',
                    apnUser: 'algar',
                    apnPassword: 'algar',
                    clearApnPassword: false,
                    commandPassword: '',
                  })
                }
              >
                Autopreencher
              </Button>
            </div>

            <div className={styles.formRow}>
              <TextField
                label="Host do servidor"
                placeholder="rastreador.exemplo.com"
                value={deviceForm.serverHost ?? ''}
                onChange={(event) =>
                  setDeviceForm({ ...deviceForm, serverHost: event.target.value })
                }
              />
              <TextField
                label="Porta TCP"
                type="number"
                value={deviceForm.serverPort ?? ''}
                onChange={(event) =>
                  setDeviceForm({
                    ...deviceForm,
                    serverPort: event.target.value ? Number(event.target.value) : null,
                  })
                }
              />
            </div>

            {!selectedChip && (
              <div className={styles.formRow}>
                <TextField
                  label="APN do chip"
                  placeholder="zap.vivo.com.br"
                  value={deviceForm.apn ?? ''}
                  onChange={(event) => setDeviceForm({ ...deviceForm, apn: event.target.value })}
                />
                <TextField
                  label="Usuário APN"
                  value={deviceForm.apnUser ?? ''}
                  onChange={(event) => setDeviceForm({ ...deviceForm, apnUser: event.target.value })}
                />
              </div>
            )}

            <div className={styles.formRow}>
              {!selectedChip && (
                <SecretField
                  label="Senha APN"
                  hint="Só se a operadora exigir."
                  isSet={Boolean(editing?.apnPasswordSet)}
                  value={deviceForm.apnPassword ?? ''}
                  clear={Boolean(deviceForm.clearApnPassword)}
                  onChange={(apnPassword) => setDeviceForm({ ...deviceForm, apnPassword })}
                  onClear={(clearApnPassword) =>
                    setDeviceForm({ ...deviceForm, clearApnPassword, apnPassword: '' })
                  }
                />
              )}
              <SecretField
                label="Senha de comando"
                hint="Apenas se o firmware exigir."
                isSet={Boolean(editing?.commandPasswordSet)}
                value={deviceForm.commandPassword ?? ''}
                clear={Boolean(deviceForm.clearCommandPassword)}
                onChange={(commandPassword) => setDeviceForm({ ...deviceForm, commandPassword })}
                onClear={(clearCommandPassword) =>
                  setDeviceForm({ ...deviceForm, clearCommandPassword, commandPassword: '' })
                }
              />
            </div>

            <div className={styles.note}>
              As senhas são só de escrita: depois de salvas não voltam para a tela nem aparecem
              no histórico de comandos (lá, e nos textos personalizados, a senha vira ***).
            </div>

            <div className={styles.note}>
              Se o seu aparelho usar um texto de comando diferente do padrão (por exemplo
              <code> RELAY,1#</code> em vez de <code>DYD#</code>), cadastre a substituição em
              &quot;comandos personalizados&quot; via API — o sistema passa a usar o seu texto sem
              precisar de nova versão.
            </div>
          </div>
        )}
      </Modal>

      {/* Comandos de configuração sugeridos (§30) */}
      <Modal
        open={provisioningFor !== null}
        wide
        title={`Configuração de ${provisioningFor?.imei ?? ''}`}
        onClose={() => setProvisioningFor(null)}
        footer={
          <Button variant="secondary" onClick={() => setProvisioningFor(null)}>
            Fechar
          </Button>
        }
      >
        {provisioning.isLoading ? (
          <Spinner inline />
        ) : (
          <div className={styles.form}>
            <div className={styles.note}>{provisioning.data?.warning}</div>
            {(provisioning.data?.commands ?? []).length === 0 ? (
              <EmptyState
                title="Sem comandos sugeridos"
                description="Preencha host, porta e intervalo no cadastro do rastreador para gerar os comandos."
              />
            ) : (
              (provisioning.data?.commands ?? []).map((command) => (
                <div key={command.type}>
                  <div className={styles.infoLabel}>{command.description}</div>
                  <div className={styles.mono}>
                    {command.available ? command.text : `indisponível — ${command.reason}`}
                  </div>
                  {command.available && command.redacted && (
                    <div className={fieldStyles.hint}>
                      Digite a senha de comando do aparelho no lugar de *** ao enviar.
                    </div>
                  )}
                </div>
              ))
            )}
          </div>
        )}
      </Modal>

      {/* Cadastro de veículo */}
      <Modal
        open={vehicleForm !== null}
        title="Veículo da central"
        onClose={() => setVehicleForm(null)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setVehicleForm(null)}>
              Cancelar
            </Button>
            <Button
              variant="primary"
              loading={saveVehicle.isPending}
              onClick={() => vehicleForm && saveVehicle.mutate(vehicleForm)}
            >
              Salvar
            </Button>
          </>
        }
      >
        {vehicleForm && (
          <div className={styles.form}>
            <p className={styles.note}>
              Veículo da própria central, sem cliente. Veículo de cliente entra pela ficha dele, em
              Novo veículo (veículo, rastreador e assinatura de uma vez).
            </p>
            <TextField
              label="Nome"
              placeholder="Subaru"
              value={vehicleForm.name}
              onChange={(event) => setVehicleForm({ ...vehicleForm, name: event.target.value })}
            />
            <div className={styles.formRow}>
              <TextField
                label="Placa"
                value={vehicleForm.plate ?? ''}
                onChange={(event) => setVehicleForm({ ...vehicleForm, plate: event.target.value })}
              />
              <TextField
                label="Limite de velocidade (km/h)"
                type="number"
                hint="Em branco usa o padrão global."
                value={vehicleForm.speedLimitKmh ?? ''}
                onChange={(event) =>
                  setVehicleForm({
                    ...vehicleForm,
                    speedLimitKmh: event.target.value ? Number(event.target.value) : null,
                  })
                }
              />
            </div>
            <SelectField
              label="Rastreador"
              value={vehicleForm.deviceId ?? ''}
              onChange={(event) =>
                setVehicleForm({ ...vehicleForm, deviceId: event.target.value || null })
              }
            >
              <option value="">sem vínculo</option>
              {(devices.data ?? []).map((device) => (
                <option key={device.id} value={device.id}>
                  {device.imei} {device.model ? `· ${device.model}` : ''}
                </option>
              ))}
            </SelectField>
          </div>
        )}
      </Modal>

      <DeviceBulkModal
        open={bulkOpen}
        onClose={() => setBulkOpen(false)}
        existingDevices={devices.data ?? []}
      />

      {activatingDevice && (
        <DeviceActivationModal
          device={activatingDevice}
          vehicle={vehicleByDevice.get(activatingDevice.id)}
          onClose={() => setActivatingDevice(null)}
          onEditDevice={() => {
            setEditing(activatingDevice);
            setDeviceForm(deviceInputFrom(activatingDevice));
          }}
        />
      )}

      <DeviceBulkActivationModal
        open={bulkActivationOpen}
        onClose={() => setBulkActivationOpen(false)}
        devices={devices.data ?? []}
        vehicles={vehicles.data ?? []}
        initialSelectedIds={Array.from(selectedDeviceIds)}
        onSuccess={() => {
          setSelectedDeviceIds(new Set());
        }}
      />
    </div>
  );
}

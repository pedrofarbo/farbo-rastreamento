import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';

import { orgApi } from '@/api/resources';
import type { VehicleInput } from '@/api/resources';
import { AssociateModal, RegisterVehicleModal } from '@/components/organization/OrgForms';
import org from '@/components/organization/Organization.module.css';
import { BackButton } from '@/components/ui/BackButton';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { Field } from '@/components/ui/Field';
import fieldStyles from '@/components/ui/Field.module.css';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { formatDateTime, formatDeviceStatus, formatRelative, formatVoltage, vehicleVoltage } from '@/services/format';
import { KIND_LABEL, dayKey, dayLabel, hourLabel } from '@/services/installations';
import { formatTaxId } from '@/services/taxid';
import { useAuth } from '@/stores/AuthContext';
import type { AssociateDetail, AssociateInput, VehicleView } from '@/types';

import styles from '../Page.module.css';
import { RequestInspectionModal } from './OrgInspectionsPage';

/** A situação do veículo para a associação: aguardando instalação, ou a conexão do rastreador. */
function vehicleStatus(v: VehicleView) {
  if (!v.device) return <Badge tone="warning">Aguardando instalação</Badge>;
  const status = v.device.status;
  return (
    <Badge tone={status === 'ONLINE' ? 'success' : status === 'STALE' ? 'warning' : 'neutral'} dot>
      {formatDeviceStatus(status)}
    </Badge>
  );
}

/** A instalação ou a retirada marcada, ou o pedido de retirada à espera da central. */
function serviceOf(a: AssociateDetail, v: VehicleView) {
  const appt = a.appointments.find((x) => x.vehicleId === v.id);
  if (appt) {
    return (
      <div className={org.muted}>
        {KIND_LABEL[appt.kind]} {appt.status === 'arrived' ? 'em andamento' : `${dayLabel(dayKey(appt.scheduledFor))} às ${hourLabel(appt.scheduledFor)}`}
      </div>
    );
  }
  const request = a.uninstallRequests[v.id];
  if (request) return <div className={org.muted}>Retirada pedida em {formatDateTime(request.requestedAt).slice(0, 10)}: a central agenda</div>;
  return null;
}

/** A ficha do associado no painel da associação: o cadastro e os veículos. */
export function OrgAssociatePage() {
  const { id = '' } = useParams<{ id: string }>();
  const { canOperateOrg, canRegisterOrg, isOrgAdmin } = useAuth();
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [editing, setEditing] = useState(false);
  const [adding, setAdding] = useState(false);
  const [confirmOff, setConfirmOff] = useState(false);
  const [inspecting, setInspecting] = useState<{ id: string; name: string } | null>(null);
  const [removing, setRemoving] = useState<VehicleView | null>(null);

  const key = ['org', 'associates', id] as const;
  // Os corretores, para o Administrador trocar no cadastro.
  const team = useQuery({ queryKey: ['org', 'team'], queryFn: orgApi.team.list, enabled: isOrgAdmin });
  const brokers = isOrgAdmin
    ? (team.data ?? []).filter((m) => m.role === 'broker' && m.active).map((m) => ({ id: m.userId, name: m.name }))
    : undefined;
  const detail = useQuery({ queryKey: key, queryFn: () => orgApi.associate(id), enabled: Boolean(id) });
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['org'] });
    void queryClient.invalidateQueries({ queryKey: ['vehicles'] });
  };

  const update = useMutation({
    mutationFn: (input: AssociateInput) => orgApi.updateAssociate(id, input),
    onSuccess: (a) => {
      setEditing(false);
      setConfirmOff(false);
      refresh();
      notify({
        tone: 'success',
        title: a.active ? `${a.name} atualizado` : `${a.name} desligado`,
        description: a.active
          ? undefined
          : 'O acesso ao app acabou na hora, os veículos saem da próxima fatura e a Farbo agenda a retirada dos rastreadores.',
      });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para salvar', description: err.message }),
  });
  const invite = useMutation({
    mutationFn: () => orgApi.inviteAssociate(id),
    onSuccess: (res) => notify({ tone: 'success', title: 'Convite enviado', description: res.message }),
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para enviar', description: err.message }),
  });
  const cancelRemoval = useMutation({
    mutationFn: (vehicleId: string) => orgApi.cancelUninstall(vehicleId),
    onSuccess: () => {
      refresh();
      notify({ tone: 'success', title: 'Pedido de retirada cancelado', description: 'Se a retirada já estava marcada, saiu da agenda.' });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para cancelar', description: err.message }),
  });
  const register = useMutation({
    mutationFn: (input: VehicleInput) => orgApi.registerVehicle(id, input),
    onSuccess: (v) => {
      setAdding(false);
      refresh();
      notify({ tone: 'success', title: `${v.name} cadastrado`, description: 'O pedido de instalação foi para a central da Farbo.' });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para cadastrar', description: err.message }),
  });

  if (detail.isLoading) return <Spinner label="Carregando o associado" />;
  const a = detail.data;
  if (!a) return <EmptyState title="Associado não encontrado" description={<Link to="/associados">Voltar</Link>} />;
  const base = { name: a.name, phone: a.phone, document: a.document };

  return (
    <div className={styles.page}>
      <div className={styles.inner}>
        <header className={styles.header}>
          <div className={styles.titleRow}>
            <BackButton to="/associados" />
            <div>
              <h1 className={styles.title}>{a.name}</h1>
              <p className={styles.description}>
                {a.email} · {a.lastSeenAt ? `entrou no app ${formatRelative(a.lastSeenAt)}` : 'ainda não entrou no app'}
              </p>
            </div>
          </div>
          {canRegisterOrg && (
            <div className={styles.actions}>
              {!a.lastSeenAt && a.active && (
                <Button variant="secondary" loading={invite.isPending} onClick={() => invite.mutate()}>
                  Reenviar convite do app
                </Button>
              )}
              <Button variant="secondary" onClick={() => setEditing(true)}>
                Editar
              </Button>
              {a.active && (
                <Button variant="primary" onClick={() => setAdding(true)}>
                  Novo veículo
                </Button>
              )}
            </div>
          )}
        </header>

        <Card title="Cadastro">
          <div className={org.stats}>
            <div className={org.stat}>
              <span className={org.statLabel}>Situação</span>
              <span>{a.active ? <Badge tone="success">Ativo</Badge> : <Badge tone="neutral">Desligado</Badge>}</span>
            </div>
            <div className={org.stat}>
              <span className={org.statLabel}>Celular</span>
              <span>{a.phone || '—'}</span>
            </div>
            <div className={org.stat}>
              <span className={org.statLabel}>CPF/CNPJ</span>
              <span>{a.document ? formatTaxId(a.document) : '—'}</span>
            </div>
            <div className={org.stat}>
              <span className={org.statLabel}>Cadastrado em</span>
              <span>{formatDateTime(a.createdAt)}</span>
            </div>
            {a.brokerName && (
              <div className={org.stat}>
                <span className={org.statLabel}>Corretor</span>
                <span>{a.brokerName}</span>
              </div>
            )}
          </div>
          {canOperateOrg && (
            <div className={styles.actions} style={{ marginTop: 'var(--space-3)', justifyContent: 'flex-start' }}>
              {confirmOff ? (
                <>
                  <span className={org.muted}>
                    Desligar {a.name}? O acesso ao app acaba na hora e os veículos saem da próxima fatura. A Farbo
                    agenda a retirada dos rastreadores (a desinstalação entra na fatura).
                  </span>
                  <Button size="small" variant="danger" loading={update.isPending} onClick={() => update.mutate({ ...base, active: false })}>
                    Desligar
                  </Button>
                  <Button size="small" variant="ghost" onClick={() => setConfirmOff(false)}>
                    Não
                  </Button>
                </>
              ) : a.active ? (
                <Button size="small" variant="ghost" onClick={() => setConfirmOff(true)}>
                  Desligar associado
                </Button>
              ) : (
                <Button size="small" variant="secondary" loading={update.isPending} onClick={() => update.mutate({ ...base, active: true })}>
                  Religar associado
                </Button>
              )}
            </div>
          )}
        </Card>

        <Card title="Veículos" subtitle="O veículo entra na fatura quando o rastreador é instalado." flush>
          {a.vehicleList.length === 0 ? (
            <EmptyState icon="🚗" title="Nenhum veículo ainda" />
          ) : (
            <div className={styles.tableWrap}>
              <table className={styles.table}>
                <thead>
                  <tr>
                    <th>Veículo</th>
                    <th>Placa</th>
                    <th>Rastreador</th>
                    <th>Voltagem</th>
                    <th>Última comunicação</th>
                    {canOperateOrg && <th aria-label="Ações" />}
                  </tr>
                </thead>
                <tbody>
                  {a.vehicleList.map((v) => (
                    <tr key={v.id}>
                      <td>
                        {v.device ? (
                          <Link to={`/veiculos/${v.id}`} className={org.rowLink}>
                            {v.name}
                          </Link>
                        ) : (
                          v.name
                        )}
                        <div className={org.muted}>{[v.brand, v.model, v.year].filter(Boolean).join(' ')}</div>
                      </td>
                      <td>{v.plate || '—'}</td>
                      <td>
                        {vehicleStatus(v)}
                        {serviceOf(a, v)}
                      </td>
                      <td>{v.device ? formatVoltage(vehicleVoltage(v)) : '—'}</td>
                      <td className={org.muted}>{v.device?.lastSeenAt ? formatRelative(v.device.lastSeenAt) : '—'}</td>
                      {canOperateOrg && (
                        <td>
                          <div className={styles.actions}>
                            <Button variant="ghost" size="small" onClick={() => setInspecting({ id: v.id, name: v.name })}>
                              Pedir vistoria
                            </Button>
                            {v.device &&
                              a.active &&
                              (a.uninstallRequests[v.id] ? (
                                <Button
                                  variant="ghost"
                                  size="small"
                                  loading={cancelRemoval.isPending && cancelRemoval.variables === v.id}
                                  onClick={() => cancelRemoval.mutate(v.id)}
                                >
                                  Cancelar a retirada
                                </Button>
                              ) : (
                                <Button variant="ghost" size="small" onClick={() => setRemoving(v)}>
                                  Pedir retirada
                                </Button>
                              ))}
                          </div>
                        </td>
                      )}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>
      </div>

      <RequestInspectionModal vehicle={inspecting} onClose={() => setInspecting(null)} />
      {removing && (
        <RequestUninstallModal
          vehicle={removing}
          onClose={() => setRemoving(null)}
          onDone={() => {
            setRemoving(null);
            refresh();
          }}
        />
      )}
      <AssociateModal
        open={editing}
        associate={a}
        saving={update.isPending}
        onClose={() => setEditing(false)}
        onSave={(input) => update.mutate(input)}
        brokers={brokers}
      />
      <RegisterVehicleModal
        open={adding}
        ownerName={a.name}
        saving={register.isPending}
        onClose={() => setAdding(false)}
        onSave={(input) => register.mutate(input)}
      />
    </div>
  );
}

/** O pedido de retirada do rastreador (o associado trocou de carro, vendeu): a central agenda. */
function RequestUninstallModal({ vehicle, onClose, onDone }: { vehicle: VehicleView; onClose: () => void; onDone: () => void }) {
  const { notify } = useToast();
  const [note, setNote] = useState('');
  const [error, setError] = useState('');
  const save = useMutation({
    mutationFn: () => orgApi.requestUninstall(vehicle.id, note),
    onSuccess: () => {
      notify({
        tone: 'success',
        title: 'Retirada pedida',
        description: 'A Farbo agenda com o associado. Quando o rastreador sair, o veículo deixa de contar e a desinstalação entra na fatura.',
      });
      onDone();
    },
    onError: (err: Error) => setError(err.message),
  });
  return (
    <Modal
      open
      title={`Pedir a retirada: ${vehicle.name}`}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancelar
          </Button>
          <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()}>
            Pedir a retirada
          </Button>
        </>
      }
    >
      <div className={styles.form}>
        {error && <div className={styles.note}>{error}</div>}
        <p className={styles.description}>
          O rastreador continua funcionando até o instalador retirar. A Farbo marca o dia com o associado e avisa no app.
        </p>
        <Field label="Motivo (opcional)">
          {(id) => (
            <textarea
              id={id}
              className={fieldStyles.textarea}
              maxLength={300}
              value={note}
              placeholder="Vendeu o carro, trocou de veículo…"
              onChange={(e) => setNote(e.target.value)}
            />
          )}
        </Field>
      </div>
    </Modal>
  );
}

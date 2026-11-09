import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { kitShipmentsApi } from '@/api/resources';
import billing from '@/components/billing/Billing.module.css';
import { Badge } from '@/components/ui/Badge';
import type { BadgeTone } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { SelectField, TextField } from '@/components/ui/Field';
import { Modal } from '@/components/ui/Modal';
import { Pager } from '@/components/ui/Pager';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { ScanButton } from '@/components/scan/ScanButton';
import fieldStyles from '@/components/ui/Field.module.css';
import { usePagedList } from '@/hooks/usePagination';
import { formatDateTime } from '@/services/format';
import { matchesSearch } from '@/services/search';
import { trackingUrl } from '@/services/fulfillment';
import type { Affiliate, KitChip, KitDevice, KitShipment, KitStatus } from '@/types';

import styles from '../../Page.module.css';
import company from '../company/Company.module.css';
import local from './Affiliates.module.css';

const kitsKey = ['kit-shipments'];

export const KIT_STATUS: Record<KitStatus, { label: string; tone: BadgeTone }> = {
  requested: { label: 'Pedido do representante', tone: 'warning' },
  preparing: { label: 'Separada', tone: 'neutral' },
  shipped: { label: 'Enviada', tone: 'accent' },
  delivered: { label: 'Entregue', tone: 'success' },
  canceled: { label: 'Cancelada', tone: 'neutral' },
};

// Primeiro o que pede ação da central: os pedidos, as separadas, as a caminho.
const ORDER: Record<KitStatus, number> = { requested: 0, preparing: 1, shipped: 2, delivered: 3, canceled: 3 };

/** As remessas na ordem da tela: o que pede ação primeiro, depois as mais novas. */
export function sortShipments(list: KitShipment[]): KitShipment[] {
  return [...list].sort((a, b) => ORDER[a.status] - ORDER[b.status] || b.createdAt.localeCompare(a.createdAt));
}

/** Os números do cartão: os pedidos esperando, os kits a caminho, com os representantes e instalados. */
export function kitTotals(list: KitShipment[]) {
  const totals = { requested: 0, inTransit: 0, stock: 0, installed: 0 };
  for (const k of list) {
    if (k.status === 'requested') totals.requested++;
    if (k.status === 'shipped') totals.inTransit += k.devices.filter((d) => !d.installed).length;
    if (k.status === 'delivered') totals.stock += k.devices.filter((d) => !d.installed).length;
    if (k.status === 'shipped' || k.status === 'delivered') totals.installed += k.devices.filter((d) => d.installed).length;
  }
  return totals;
}

/** Onde a busca procura o rastreador do kit: o IMEI, o chip e o veículo em que foi instalado. */
const kitDeviceFields = (d: KitDevice) => [d.imei, d.iccid, d.vehicleName, d.plate, d.customerName];

/** A remessa na busca: o representante, o rastreio, a observação, um dos rastreadores ou um dos chips avulsos. */
export function shipmentMatches(k: KitShipment, term: string): boolean {
  return (
    matchesSearch(term, [k.affiliateName, k.trackingCode, k.carrier, k.notes, k.requestNote]) ||
    k.devices.some((d) => matchesSearch(term, kitDeviceFields(d))) ||
    (k.chips ?? []).some((c) => matchesSearch(term, [c.iccid, c.deviceImei]))
  );
}

/** "2 rastreadores e 1 chip": o que vai na remessa. */
export function kitItemsLabel(devices: number, chips: number): string {
  const parts = [
    devices > 0 ? `${devices} ${devices === 1 ? 'rastreador' : 'rastreadores'}` : '',
    chips > 0 ? `${chips} ${chips === 1 ? 'chip' : 'chips'}` : '',
  ].filter(Boolean);
  return parts.join(' e ');
}

/** "enviada em 03/11/2026 14:00": quando a remessa chegou à situação de agora. */
function statusWhen(k: KitShipment): string {
  switch (k.status) {
    case 'shipped':
      return `em ${formatDateTime(k.shippedAt)}`;
    case 'delivered':
      return `em ${formatDateTime(k.deliveredAt)}`;
    case 'canceled':
      return `em ${formatDateTime(k.canceledAt)}`;
    default:
      return `desde ${formatDateTime(k.createdAt)}`;
  }
}

/**
 * Os kits de instalação dos representantes: a central separa os rastreadores
 * (com o chip) para o representante que entrega em mãos e instala, envia com
 * a transportadora e o código de rastreio e acompanha até ele confirmar, pela
 * página dele, que recebeu. Os pedidos que ele faz pela página vêm primeiro.
 */
export function KitShipmentsCard({ affiliates }: { affiliates: Affiliate[] }) {
  const { notify } = useToast();
  const queryClient = useQueryClient();
  const shipments = useQuery({ queryKey: kitsKey, queryFn: kitShipmentsApi.list });
  const list = useMemo(() => sortShipments(shipments.data ?? []), [shipments.data]);
  const [search, setSearch] = useState('');
  const term = search.trim();
  const shown = useMemo(() => (term ? list.filter((k) => shipmentMatches(k, term)) : list), [list, term]);
  const paged = usePagedList(shown, 'kit-shipments', term);
  const totals = kitTotals(list);
  // Montando (nova ou atendendo um pedido) e enviando: uma janela por vez.
  const [picking, setPicking] = useState<KitShipment | 'new' | null>(null);
  const [shipping, setShipping] = useState<KitShipment | null>(null);
  const [canceling, setCanceling] = useState<string | null>(null);

  const done = (title: string, description?: string) => {
    void queryClient.invalidateQueries({ queryKey: kitsKey });
    notify({ tone: 'success', title, description });
  };
  const failed = (title: string) => (err: Error) => notify({ tone: 'error', title, description: err.message });
  const deliver = useMutation({
    mutationFn: (id: string) => kitShipmentsApi.deliver(id),
    onSuccess: (k) => done('Remessa entregue', `${k.devices.length} rastreadores no estoque de ${k.affiliateName}.`),
    onError: failed('Não foi possível marcar a entrega'),
  });
  const cancel = useMutation({
    mutationFn: (id: string) => kitShipmentsApi.cancel(id),
    onSuccess: () => {
      setCanceling(null);
      done('Remessa cancelada', 'Os rastreadores dela voltaram a ficar livres.');
    },
    onError: failed('Não foi possível cancelar'),
  });

  return (
    <Card
      flush
      title="Kits dos representantes"
      subtitle="Os rastreadores (com o chip, ou com o chip avulso para encaixar na ativação) que vão para os representantes que entregam em mãos e instalam. Eles acompanham pela página e confirmam quando recebem; instalar e ativar baixam o estoque sozinhos."
      actions={
        <Button size="small" variant="primary" onClick={() => setPicking('new')}>
          Enviar kits
        </Button>
      }
    >
      <div className={`${billing.tiles} ${local.kitTiles}`}>
        <div className={billing.tile}>
          <span className={billing.tileLabel}>Pedidos esperando</span>
          <span className={billing.tileValue}>{totals.requested}</span>
          <span className={billing.tileHint}>feitos pela página do representante</span>
        </div>
        <div className={billing.tile}>
          <span className={billing.tileLabel}>A caminho</span>
          <span className={billing.tileValue}>{totals.inTransit}</span>
          <span className={billing.tileHint}>rastreadores enviados</span>
        </div>
        <div className={billing.tile}>
          <span className={billing.tileLabel}>Com os representantes</span>
          <span className={billing.tileValue}>{totals.stock}</span>
          <span className={billing.tileHint}>recebidos, ainda sem veículo</span>
        </div>
        <div className={billing.tile}>
          <span className={billing.tileLabel}>Instalados</span>
          <span className={billing.tileValue}>{totals.installed}</span>
          <span className={billing.tileHint}>dos kits, já num veículo</span>
        </div>
      </div>

      {shipments.isLoading ? (
        <Spinner label="Carregando as remessas" />
      ) : shipments.isError ? (
        <p className={`${company.danger} ${local.kitPad}`}>{(shipments.error as Error).message}</p>
      ) : list.length === 0 ? (
        <EmptyState
          icon="📦"
          title="Nenhuma remessa ainda"
          description="Separe os rastreadores para um representante em Enviar kits. Os pedidos que ele fizer pela página aparecem aqui."
        />
      ) : (
        <>
          <div className={local.kitPad}>
            <div className={`${fieldStyles.adorned} ${styles.listSearch}`}>
              <input
                type="search"
                className={fieldStyles.input}
                placeholder="Representante, IMEI, chip ou rastreio"
                aria-label="Buscar remessa ou rastreador"
                autoComplete="off"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
              />
              <ScanButton kind="imei" onRead={setSearch} />
            </div>
          </div>
          {shown.length === 0 ? (
            <EmptyState icon="🔎" title={`Nenhuma remessa com “${term}”`} />
          ) : (
            <>
              <div className={styles.tableWrap}>
                <table className={`${styles.table} ${styles.stackTable}`}>
                  <thead>
                    <tr>
                      <th>Representante</th>
                      <th>Situação</th>
                      <th>Rastreadores</th>
                      <th>Envio</th>
                      <th />
                    </tr>
                  </thead>
                  <tbody>
                    {paged.items.map((k) => (
                      <tr key={k.id}>
                        <td data-label="Representante">
                          <span className={local.who}>
                            <strong>{k.affiliateName}</strong>
                            {k.requestedQuantity != null && (
                              <span className={company.muted}>
                                pediu {k.requestedQuantity} {k.requestedQuantity === 1 ? 'kit' : 'kits'}
                                {k.requestNote && `: “${k.requestNote}”`}
                              </span>
                            )}
                            {k.notes && <span className={company.muted}>Obs.: {k.notes}</span>}
                          </span>
                        </td>
                        <td data-label="Situação">
                          <Badge tone={KIT_STATUS[k.status].tone}>{KIT_STATUS[k.status].label}</Badge>
                          <div className={company.muted}>{statusWhen(k)}</div>
                        </td>
                        <td data-label="Rastreadores">
                          {k.devices.length === 0 && (k.chips ?? []).length === 0 ? (
                            <span className={company.muted}>{k.status === 'requested' ? 'a separar' : '—'}</span>
                          ) : k.devices.length === 0 ? null : (
                            <details
                              className={local.kitDevices}
                              open={Boolean(term) && k.devices.some((d) => matchesSearch(term, kitDeviceFields(d)))}
                            >
                              <summary>
                                {k.devices.length} {k.devices.length === 1 ? 'rastreador' : 'rastreadores'}
                                {k.devices.some((d) => d.installed) && ` · ${k.devices.filter((d) => d.installed).length} instalados`}
                              </summary>
                              <ul>
                                {k.devices.map((d) => (
                                  <li key={d.id}>
                                    <span className={local.mono}>{d.imei}</span>
                                    {d.iccid && <div className={company.muted}>chip {d.iccid}</div>}
                                    {d.installed && (
                                      <div className={company.muted}>
                                        no {d.vehicleName}
                                        {d.plate && ` (${d.plate})`}
                                        {d.customerName && ` · ${d.customerName}`}
                                      </div>
                                    )}
                                  </li>
                                ))}
                              </ul>
                            </details>
                          )}
                          {(k.chips ?? []).length > 0 && (
                            <KitChipList
                              chips={k.chips ?? []}
                              open={Boolean(term) && (k.chips ?? []).some((c) => matchesSearch(term, [c.iccid, c.deviceImei]))}
                            />
                          )}
                        </td>
                        <td data-label="Envio">
                          {k.carrier ? (
                            <span className={local.who}>
                              <span>{k.carrier}</span>
                              {k.trackingCode && (
                                <a href={trackingUrl(k.trackingCode)} target="_blank" rel="noreferrer" className={local.mono}>
                                  {k.trackingCode}
                                </a>
                              )}
                            </span>
                          ) : (
                            <span className={company.muted}>—</span>
                          )}
                        </td>
                        <td className={company.right}>
                          {canceling === k.id ? (
                            <span className={local.confirm}>
                              Cancelar a remessa?
                              <Button size="small" variant="danger" loading={cancel.isPending} onClick={() => cancel.mutate(k.id)}>
                                Sim, cancelar
                              </Button>
                              <Button size="small" variant="ghost" onClick={() => setCanceling(null)}>
                                Não
                              </Button>
                            </span>
                          ) : (
                            <span className={local.actions}>
                              {k.status === 'requested' && (
                                <Button size="small" variant="primary" onClick={() => setPicking(k)}>
                                  Atender
                                </Button>
                              )}
                              {k.status === 'preparing' && (
                                <Button size="small" variant="primary" onClick={() => setShipping(k)}>
                                  Marcar como enviada
                                </Button>
                              )}
                              {k.status === 'shipped' && (
                                <Button
                                  size="small"
                                  variant="secondary"
                                  loading={deliver.isPending && deliver.variables === k.id}
                                  onClick={() => deliver.mutate(k.id)}
                                >
                                  Marcar como entregue
                                </Button>
                              )}
                              {(k.status === 'requested' || k.status === 'preparing' || k.status === 'shipped') && (
                                <Button size="small" variant="ghost" onClick={() => setCanceling(k.id)}>
                                  Cancelar
                                </Button>
                              )}
                            </span>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <Pager {...paged.pager} />
            </>
          )}
        </>
      )}

      {picking && (
        <KitDevicesModal
          request={picking === 'new' ? null : picking}
          affiliates={affiliates}
          onClose={() => setPicking(null)}
          onSaved={(k) => {
            setPicking(null);
            done(
              `${k.devices.length} ${k.devices.length === 1 ? 'rastreador separado' : 'rastreadores separados'} para ${k.affiliateName}`,
              'Quando despachar, marque como enviada com o código de rastreio.',
            );
          }}
        />
      )}
      {shipping && (
        <ShipModal
          shipment={shipping}
          onClose={() => setShipping(null)}
          onSaved={(k) => {
            setShipping(null);
            done('Remessa enviada', `${k.affiliateName} acompanha pela página e confirma quando receber.`);
          }}
        />
      )}
    </Card>
  );
}

/** Os chips avulsos da remessa: o ICCID, a operadora e, já usado, o rastreador em que foi. */
function KitChipList({ chips, open }: { chips: KitChip[]; open: boolean }) {
  const used = chips.filter((c) => c.used).length;
  return (
    <details className={local.kitDevices} open={open}>
      <summary>
        {chips.length} {chips.length === 1 ? 'chip avulso' : 'chips avulsos'}
        {used > 0 && ` · ${used} ${used === 1 ? 'usado' : 'usados'}`}
      </summary>
      <ul>
        {chips.map((c) => (
          <li key={c.id}>
            <span className={local.mono}>{c.iccid}</span>
            {c.carrier && <div className={company.muted}>{c.carrier}</div>}
            {c.used && <div className={company.muted}>no rastreador {c.deviceImei}</div>}
          </li>
        ))}
      </ul>
    </details>
  );
}

/**
 * O que vai no kit: o representante (na remessa nova), os rastreadores livres
 * — sem veículo e fora de outra remessa — e os chips avulsos livres (para os
 * rastreadores que vão sem chip: o instalador encaixa na ativação), com a
 * busca pelo IMEI ou pelo chip. Atendendo um pedido, o representante e a
 * quantidade vêm dele.
 */
function KitDevicesModal({
  request,
  affiliates,
  onClose,
  onSaved,
}: {
  request: KitShipment | null;
  affiliates: Affiliate[];
  onClose: () => void;
  onSaved: (k: KitShipment) => void;
}) {
  const devices = useQuery({ queryKey: [...kitsKey, 'available'], queryFn: kitShipmentsApi.availableDevices });
  const freeChips = useQuery({ queryKey: [...kitsKey, 'available-chips'], queryFn: kitShipmentsApi.availableChips });
  // Os que entregam em mãos primeiro: são os que recebem kits.
  const options = useMemo(
    () =>
      affiliates
        .filter((a) => a.active)
        .sort((a, b) => Number(!!b.handDelivery) - Number(!!a.handDelivery) || a.name.localeCompare(b.name, 'pt-BR')),
    [affiliates],
  );
  const [affiliateId, setAffiliateId] = useState('');
  const [search, setSearch] = useState('');
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [chipSearch, setChipSearch] = useState('');
  const [chipsChosen, setChipsChosen] = useState<Set<string>>(new Set());
  const [notes, setNotes] = useState(request?.notes ?? '');

  const all = devices.data ?? [];
  const term = search.replace(/\s/g, '');
  const shown = term ? all.filter((d) => d.imei.includes(term) || d.iccid.includes(term)) : all;
  const flip = (set: Set<string>, id: string) => {
    const next = new Set(set);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    return next;
  };
  const toggle = (id: string) => setSelected((s) => flip(s, id));
  const chips = freeChips.data ?? [];
  const chipTerm = chipSearch.replace(/\s/g, '');
  const shownChips = chipTerm ? chips.filter((c) => c.iccid.includes(chipTerm)) : chips;
  // Os rastreadores marcados que vão sem chip: cada um pede um chip avulso.
  const withoutChip = all.filter((d) => selected.has(d.id) && !d.iccid).length;
  const missingChips = Math.max(0, withoutChip - chipsChosen.size);

  const save = useMutation({
    mutationFn: () => {
      const deviceIds = [...selected];
      const chipIds = [...chipsChosen];
      return request
        ? kitShipmentsApi.prepare(request.id, { deviceIds, chipIds, notes: notes.trim() })
        : kitShipmentsApi.create({ affiliateId, deviceIds, chipIds, notes: notes.trim() });
    },
    onSuccess: onSaved,
  });

  const wanted = request?.requestedQuantity ?? null;
  return (
    <Modal
      open
      wide
      title={request ? `Atender o pedido de ${request.affiliateName}` : 'Enviar kits para um representante'}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Voltar
          </Button>
          <Button
            variant="primary"
            loading={save.isPending}
            disabled={(selected.size === 0 && chipsChosen.size === 0) || (!request && !affiliateId)}
            onClick={() => save.mutate()}
          >
            Separar {kitItemsLabel(selected.size, chipsChosen.size) || 'o kit'}
          </Button>
        </>
      }
    >
      <div className={local.modal}>
        {request ? (
          <p className={company.muted}>
            Pediu {wanted} {wanted === 1 ? 'kit' : 'kits'} em {formatDateTime(request.createdAt)}
            {request.requestNote && `: “${request.requestNote}”`}.
          </p>
        ) : (
          <SelectField label="Representante" value={affiliateId} onChange={(e) => setAffiliateId(e.target.value)}>
            <option value="">Escolha…</option>
            {options.map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}
                {a.handDelivery ? ' · entrega em mãos' : ''}
              </option>
            ))}
          </SelectField>
        )}
        <TextField
          label="Buscar rastreador"
          inputMode="numeric"
          placeholder="IMEI ou número do chip"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        {devices.isLoading ? (
          <Spinner label="Carregando os rastreadores livres" />
        ) : devices.isError ? (
          <p className={company.danger}>{(devices.error as Error).message}</p>
        ) : all.length === 0 ? (
          <p className={company.muted}>
            Nenhum rastreador livre: cadastre os rastreadores em Rastreadores (os que estão num veículo ou em outra remessa
            não entram).
          </p>
        ) : (
          <>
            <span className={local.linkButtons}>
              <span className={company.muted}>
                {selected.size} de {all.length} livres selecionados
                {wanted != null && selected.size !== wanted && ` · o pedido é de ${wanted}`}
              </span>
              {wanted != null && selected.size !== wanted && shown.length > 0 && (
                <Button size="small" variant="ghost" onClick={() => setSelected(new Set(shown.slice(0, wanted).map((d) => d.id)))}>
                  Marcar {Math.min(wanted, shown.length)} da lista
                </Button>
              )}
            </span>
            {shown.length === 0 ? (
              <p className={company.muted}>Nenhum rastreador livre com “{search}”.</p>
            ) : (
              <ul className={local.deviceList} aria-label="Rastreadores livres">
                {shown.map((d) => (
                  <li key={d.id}>
                    <label className={company.check}>
                      <input type="checkbox" checked={selected.has(d.id)} onChange={() => toggle(d.id)} />
                      <span className={local.mono}>{d.imei}</span>
                      <span className={company.muted}>{d.iccid ? `chip ${d.iccid}` : 'sem chip'}</span>
                    </label>
                  </li>
                ))}
              </ul>
            )}
          </>
        )}
        <div className={local.kitChips}>
          <strong>Chips avulsos</strong>
          <span className={company.muted}>
            Para os rastreadores que vão sem chip: o instalador do representante encaixa o chip na hora de ativar.
          </span>
          {withoutChip > 0 && (
            <span className={missingChips > 0 ? company.danger : company.muted}>
              {withoutChip === 1 ? '1 rastreador marcado vai sem chip' : `${withoutChip} rastreadores marcados vão sem chip`}
              {missingChips > 0
                ? `: marque ${missingChips === 1 ? 'mais 1 chip' : `mais ${missingChips} chips`}.`
                : ': os chips marcados vão junto.'}
            </span>
          )}
          {freeChips.isLoading ? (
            <Spinner label="Carregando os chips livres" />
          ) : freeChips.isError ? (
            <p className={company.danger}>{(freeChips.error as Error).message}</p>
          ) : chips.length === 0 ? (
            <p className={company.muted}>Nenhum chip livre: cadastre os chips em Chips (os que estão num rastreador ou em outra remessa não entram).</p>
          ) : (
            <>
              <TextField
                label="Buscar chip"
                inputMode="numeric"
                placeholder="Números do ICCID"
                value={chipSearch}
                onChange={(e) => setChipSearch(e.target.value)}
              />
              <span className={local.linkButtons}>
                <span className={company.muted}>
                  {chipsChosen.size} de {chips.length} livres selecionados
                </span>
                {missingChips > 0 && shownChips.some((c) => !chipsChosen.has(c.id)) && (
                  <Button
                    size="small"
                    variant="ghost"
                    onClick={() =>
                      setChipsChosen((set) => {
                        const next = new Set(set);
                        for (const c of shownChips) {
                          if (next.size >= withoutChip) break;
                          next.add(c.id);
                        }
                        return next;
                      })
                    }
                  >
                    Marcar {Math.min(missingChips, shownChips.filter((c) => !chipsChosen.has(c.id)).length)} da lista
                  </Button>
                )}
              </span>
              {shownChips.length === 0 ? (
                <p className={company.muted}>Nenhum chip livre com “{chipSearch}”.</p>
              ) : (
                <ul className={local.deviceList} aria-label="Chips livres">
                  {shownChips.map((c) => (
                    <li key={c.id}>
                      <label className={company.check}>
                        <input type="checkbox" checked={chipsChosen.has(c.id)} onChange={() => setChipsChosen((set) => flip(set, c.id))} />
                        <span className={local.mono}>{c.iccid}</span>
                        {c.carrier && <span className={company.muted}>{c.carrier}</span>}
                      </label>
                    </li>
                  ))}
                </ul>
              )}
            </>
          )}
        </div>
        <TextField
          label="Observação (opcional)"
          maxLength={1000}
          value={notes}
          onChange={(e) => setNotes(e.target.value)}
          placeholder="Ex.: com os chicotes e o manual"
        />
        {save.isError && <p className={company.danger}>{(save.error as Error).message}</p>}
      </div>
    </Modal>
  );
}

/** Despachar a remessa: a transportadora e o código de rastreio (ou em mãos, sem código). */
function ShipModal({ shipment, onClose, onSaved }: { shipment: KitShipment; onClose: () => void; onSaved: (k: KitShipment) => void }) {
  const [carrier, setCarrier] = useState('Correios SEDEX');
  const [code, setCode] = useState('');
  const save = useMutation({
    mutationFn: () => kitShipmentsApi.ship(shipment.id, { carrier: carrier.trim(), trackingCode: code.trim() }),
    onSuccess: onSaved,
  });
  return (
    <Modal
      open
      title={`Enviar a remessa de ${shipment.affiliateName}`}
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Voltar
          </Button>
          <Button variant="primary" loading={save.isPending} disabled={!carrier.trim()} onClick={() => save.mutate()}>
            Marcar como enviada
          </Button>
        </>
      }
    >
      <div className={local.modal}>
        <p className={company.muted}>
          {shipment.devices.length} {shipment.devices.length === 1 ? 'rastreador' : 'rastreadores'}. O representante vê o
          código de rastreio na página dele.
        </p>
        <TextField
          label="Transportadora"
          list="kit-carriers"
          maxLength={80}
          value={carrier}
          onChange={(e) => setCarrier(e.target.value)}
        />
        <datalist id="kit-carriers">
          <option value="Correios SEDEX" />
          <option value="Correios PAC" />
          <option value="Jadlog" />
          <option value="Loggi" />
          <option value="Em mãos" />
        </datalist>
        <TextField
          label="Código de rastreio"
          hint="Em branco se for entregue em mãos."
          maxLength={60}
          value={code}
          onChange={(e) => setCode(e.target.value.toUpperCase())}
          placeholder="Ex.: QB123456789BR"
        />
        {save.isError && <p className={company.danger}>{(save.error as Error).message}</p>}
      </div>
    </Modal>
  );
}

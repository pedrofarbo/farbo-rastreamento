import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { devicesApi, installerLinksApi } from '@/api/resources';
import styles from '@/components/install/Appointments.module.css';
import { ScanButton } from '@/components/scan/ScanButton';
import { Badge } from '@/components/ui/Badge';
import type { BadgeTone } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { Field, SelectField, TextField } from '@/components/ui/Field';
import fieldStyles from '@/components/ui/Field.module.css';
import { Modal } from '@/components/ui/Modal';
import { Pager } from '@/components/ui/Pager';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { whatsappLink } from '@/config/contact';
import { usePagedList } from '@/hooks/usePagination';
import { formatDateTime, formatRelative } from '@/services/format';
import { installerLinkMessage } from '@/services/installations';
import type { Device, Installer, InstallerLink } from '@/types';

import pageStyles from '../Page.module.css';

/*
 * A instalação avulsa (na tela de Prestadores): a central escolhe o
 * rastreador e manda o link ao instalador, sem agendamento nem veículo. Pelo
 * link, ele testa o sinal e conclui; o link expira na conclusão ou quando a
 * central encerra.
 */

export const installerLinksKey = ['installer-links'] as const;

/** O link no WhatsApp: com o número do prestador (da lista) ou para escolher a conversa. */
function linkWhatsapp(l: InstallerLink, installers: Installer[]): string {
  const text = installerLinkMessage(l);
  const phone = installers.find((i) => i.id === l.installerId)?.whatsapp ?? '';
  return phone ? whatsappLink(phone, text) : `https://wa.me/?text=${encodeURIComponent(text)}`;
}

/** A situação do link, com o último teste do sinal enquanto aberto. */
function statusOf(l: InstallerLink): { label: string; tone: BadgeTone } {
  if (l.status === 'done') return { label: 'Concluída', tone: 'success' };
  if (l.status === 'closed') return { label: 'Encerrado', tone: 'neutral' };
  if (!l.checkedAt) return { label: 'Aguardando o teste', tone: 'accent' };
  return l.checkReady ? { label: 'Sinal OK', tone: 'success' } : { label: 'Sem sinal no teste', tone: 'warning' };
}

/** O rastreador como aparece na escolha e na lista. */
const deviceLabel = (d: Pick<Device, 'imei' | 'model' | 'manufacturer'>) =>
  [d.imei, [d.manufacturer, d.model].filter(Boolean).join(' ')].filter(Boolean).join(' · ');

/** Os links abertos e os encerrados nos últimos 30 dias. */
export function InstallerLinksCard({ installers }: { installers: Installer[] }) {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const links = useQuery({ queryKey: installerLinksKey, queryFn: installerLinksApi.list, refetchInterval: 30_000 });
  const close = useMutation({
    mutationFn: installerLinksApi.close,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: installerLinksKey });
      notify({ tone: 'success', title: 'Link encerrado', description: 'O instalador não abre mais.' });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não deu para encerrar', description: err.message }),
  });
  const copy = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      notify({ tone: 'success', title: 'Link copiado' });
    } catch {
      /* sem permissão: a pessoa copia na mão */
    }
  };
  const list = links.data ?? [];
  const open = list.filter((l) => l.status === 'open').length;
  const paged = usePagedList(list, 'installer-links');

  return (
    <Card
      title="Instalações avulsas"
      subtitle={
        open > 0
          ? `${open} link(s) aberto(s). O link expira quando o instalador conclui ou quando você encerra.`
          : 'O link do rastreador escolhido, sem agendamento: o instalador testa o sinal e conclui.'
      }
      flush
    >
      {links.isLoading ? (
        <Spinner label="Carregando" />
      ) : list.length === 0 ? (
        <p className={styles.empty}>Nenhum link gerado nos últimos 30 dias.</p>
      ) : (
        <>
          <div className={pageStyles.tableWrap}>
            <table className={`${pageStyles.table} ${pageStyles.stackTable}`}>
              <thead>
                <tr>
                  <th>Rastreador</th>
                  <th>Instalador</th>
                  <th>Situação</th>
                  <th>Gerado</th>
                  <th aria-label="Ações" />
                </tr>
              </thead>
              <tbody>
                {paged.items.map((l) => {
                  const status = statusOf(l);
                  const vehicle = [l.vehicleName, l.plate].filter(Boolean).join(' · ');
                  return (
                    <tr key={l.id}>
                      <td>
                        {/* Um bloco só: no celular a célula vira linha flexível. */}
                        <div>
                          <strong>{l.imei}</strong>
                          <div className={styles.muted}>{[l.deviceModel, vehicle].filter(Boolean).join(' · ') || '—'}</div>
                          {l.notes && <div className={styles.muted}>📝 {l.notes}</div>}
                        </div>
                      </td>
                      <td data-label="Instalador">{l.installerName || '—'}</td>
                      <td data-label="Situação">
                        <div>
                          <Badge tone={status.tone} dot={l.status === 'open'}>
                            {status.label}
                          </Badge>
                          <div className={styles.muted}>
                            {l.status === 'done'
                              ? formatDateTime(l.completedAt)
                              : l.status === 'closed'
                                ? formatDateTime(l.closedAt)
                                : l.checkedAt
                                  ? `testado ${formatRelative(l.checkedAt)}`
                                  : ''}
                          </div>
                          {l.installerNote && <div className={styles.muted}>“{l.installerNote}”</div>}
                        </div>
                      </td>
                      <td data-label="Gerado">{formatDateTime(l.createdAt)}</td>
                      <td>
                        {l.status === 'open' && (
                          <div className={styles.actions}>
                            <a className={styles.waLink} href={linkWhatsapp(l, installers)} target="_blank" rel="noreferrer noopener">
                              WhatsApp
                            </a>
                            {l.url && (
                              <Button size="small" variant="ghost" onClick={() => void copy(l.url ?? '')}>
                                Copiar link
                              </Button>
                            )}
                            <Button
                              size="small"
                              variant="ghost"
                              loading={close.isPending && close.variables === l.id}
                              onClick={() => close.mutate(l.id)}
                            >
                              Encerrar
                            </Button>
                          </div>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
          <Pager {...paged.pager} />
        </>
      )}
    </Card>
  );
}

/**
 * Gera o link: o rastreador (a busca pelo IMEI ou pelo modelo, ou a etiqueta
 * pela câmera), o instalador e as observações; depois, o link para mandar.
 */
export function InstallerLinkModal({
  installers,
  installerId: initialInstaller = '',
  onClose,
}: {
  installers: Installer[];
  installerId?: string;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const devices = useQuery({ queryKey: ['devices'], queryFn: devicesApi.list });
  const links = useQuery({ queryKey: installerLinksKey, queryFn: installerLinksApi.list });
  const [search, setSearch] = useState('');
  const [deviceId, setDeviceId] = useState('');
  const [installerId, setInstallerId] = useState(initialInstaller);
  const [installerName, setInstallerName] = useState('');
  const [notes, setNotes] = useState('');
  const [error, setError] = useState('');
  const [saved, setSaved] = useState<InstallerLink | null>(null);

  // Os rastreadores com link aberto não entram de novo (um por rastreador).
  const busy = new Set((links.data ?? []).filter((l) => l.status === 'open').map((l) => l.deviceId));
  const term = search.trim().toLowerCase();
  const digits = search.replace(/\D/g, '');
  const all = devices.data ?? [];
  const options = term
    ? all.filter(
        (d) =>
          d.id === deviceId ||
          (digits.length >= 3 && d.imei.includes(digits)) ||
          `${d.manufacturer} ${d.model} ${d.notes}`.toLowerCase().includes(term),
      )
    : all;

  const pickSearch = (text: string) => {
    setSearch(text);
    // O IMEI inteiro (digitado ou lido da etiqueta) já escolhe o rastreador.
    const exact = all.find((d) => d.imei === text.replace(/\D/g, ''));
    if (exact && !busy.has(exact.id)) setDeviceId(exact.id);
  };

  const save = useMutation({
    mutationFn: () => {
      const picked = installers.find((i) => i.id === installerId);
      return installerLinksApi.create({
        deviceId,
        installerId: picked ? picked.id : null,
        installerName: picked ? picked.name : installerName.trim(),
        notes,
      });
    },
    onSuccess: (l) => {
      void queryClient.invalidateQueries({ queryKey: installerLinksKey });
      notify({ tone: 'success', title: 'Link gerado', description: `Rastreador ${l.imei}` });
      setSaved(l);
    },
    onError: (err: Error) => setError(err.message),
  });

  if (saved) {
    return (
      <Modal
        open
        title="Mande o link ao instalador"
        onClose={onClose}
        footer={
          <Button variant="primary" onClick={onClose}>
            Pronto
          </Button>
        }
      >
        <div className={pageStyles.form}>
          <p className={pageStyles.description}>
            Rastreador {saved.imei}. Pelo link, o instalador testa o sinal e conclui, no celular e sem login. O link expira
            quando ele concluir ou quando você encerrar em “Instalações avulsas”.
          </p>
          <div className={styles.link}>
            <span className={styles.url}>{saved.url}</span>
            <div className={styles.actions} style={{ justifyContent: 'flex-start' }}>
              <a className={styles.waLink} href={linkWhatsapp(saved, installers)} target="_blank" rel="noreferrer noopener">
                Enviar pelo WhatsApp
              </a>
              <Button
                size="small"
                variant="ghost"
                onClick={() =>
                  void navigator.clipboard?.writeText(saved.url ?? '').then(() => notify({ tone: 'success', title: 'Link copiado' }))
                }
              >
                Copiar
              </Button>
            </div>
          </div>
        </div>
      </Modal>
    );
  }

  return (
    <Modal
      open
      title="Link de instalação avulsa"
      onClose={onClose}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancelar
          </Button>
          <Button
            variant="primary"
            loading={save.isPending}
            disabled={!deviceId || (!installerId && !installerName.trim())}
            onClick={() => {
              setError('');
              save.mutate();
            }}
          >
            Gerar o link
          </Button>
        </>
      }
    >
      <div className={pageStyles.form}>
        {error && <div className={pageStyles.note}>{error}</div>}
        <TextField
          label="Buscar o rastreador"
          placeholder="Parte do IMEI ou o modelo"
          hint="O botão no campo lê o IMEI da etiqueta pela câmera."
          inputMode="search"
          autoComplete="off"
          value={search}
          onChange={(e) => pickSearch(e.target.value)}
          action={<ScanButton kind="imei" onRead={pickSearch} />}
        />
        <SelectField label="Rastreador" value={deviceId} onChange={(e) => setDeviceId(e.target.value)}>
          <option value="">
            {devices.isLoading ? 'Carregando…' : options.length === 0 ? 'Nenhum rastreador encontrado' : 'Escolha o rastreador'}
          </option>
          {options.map((d) => (
            <option key={d.id} value={d.id} disabled={busy.has(d.id)}>
              {deviceLabel(d)}
              {busy.has(d.id) ? ' (link aberto)' : ''}
            </option>
          ))}
        </SelectField>
        <SelectField label="Instalador" value={installerId} onChange={(e) => setInstallerId(e.target.value)}>
          <option value="">Outro (digitar o nome)</option>
          {installers.map((i) => (
            <option key={i.id} value={i.id}>
              {i.name}
              {i.city ? ` · ${i.city}` : ''}
            </option>
          ))}
        </SelectField>
        {!installerId && (
          <TextField label="Nome do instalador" maxLength={120} value={installerName} onChange={(e) => setInstallerName(e.target.value)} />
        )}
        <Field label="Observações para o instalador">
          {(id) => (
            <textarea
              id={id}
              className={fieldStyles.textarea}
              maxLength={1000}
              value={notes}
              placeholder="De quem é o veículo, onde instalar…"
              onChange={(e) => setNotes(e.target.value)}
            />
          )}
        </Field>
      </div>
    </Modal>
  );
}

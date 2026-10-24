import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { liveLinksApi } from '@/api/resources';
import { IdentityCheck } from '@/components/auth/IdentityCheck';
import { Button } from '@/components/ui/Button';
import { TextField } from '@/components/ui/Field';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { durationLabel, liveShareText, untilLabel, viewsLabel } from '@/services/liveLinks';
import { useAuth } from '@/stores/AuthContext';
import type { LiveLink, VehicleView } from '@/types';

import styles from './LiveLinkPanel.module.css';

export const liveLinksKey = (vehicleId: string) => ['live-links', vehicleId] as const;

/**
 * A localização ao vivo por um tempo: o dono gera um link que mostra onde o
 * veículo está, sem login, por algumas horas (a família numa viagem, quem vai
 * receber uma entrega). Gerar pede a biometria ou a senha; o link morre no
 * prazo ou quando o dono encerra.
 */
export function LiveLinkPanel({ vehicle }: { vehicle: VehicleView }) {
  const { isCustomer, canOperateAccount } = useAuth();
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [hours, setHours] = useState(2);
  const [label, setLabel] = useState('');
  const [verifying, setVerifying] = useState(false);
  const [error, setError] = useState('');
  const allowed = isCustomer && canOperateAccount && Boolean(vehicle.device) && !vehicle.shared;

  const list = useQuery({
    queryKey: liveLinksKey(vehicle.id),
    queryFn: () => liveLinksApi.list(vehicle.id),
    enabled: allowed,
    // As visitas e o prazo andam sozinhos.
    refetchInterval: (query) => ((query.state.data?.links.length ?? 0) > 0 ? 60_000 : false),
  });

  useEffect(() => {
    setVerifying(false);
    setError('');
  }, [vehicle.id]);

  const refresh = () => queryClient.invalidateQueries({ queryKey: liveLinksKey(vehicle.id) });

  const share = async (link: LiveLink) => {
    const text = liveShareText(vehicle, link);
    try {
      if (navigator.share) {
        await navigator.share({ title: `${vehicle.name} ao vivo`, text, url: link.url });
        return;
      }
      await navigator.clipboard.writeText(text);
      notify({ tone: 'success', title: 'Link copiado', description: 'Cole na conversa com quem vai acompanhar.' });
    } catch {
      /* o cliente cancelou */
    }
  };

  const create = useMutation({
    mutationFn: (token: string) => liveLinksApi.create(vehicle.id, { hours, label: label.trim() }, token),
    onSuccess: (link) => {
      setVerifying(false);
      setLabel('');
      setError('');
      void refresh();
      notify({ tone: 'success', title: 'Link gerado', description: `Vale ${untilLabel(link.expiresAt)}.` });
      void share(link);
    },
    onError: (err: Error) => {
      setVerifying(false);
      setError(err.message);
    },
  });

  const revoke = useMutation({
    mutationFn: (link: LiveLink) => liveLinksApi.revoke(vehicle.id, link.id),
    onSuccess: () => {
      void refresh();
      notify({ tone: 'success', title: 'Link encerrado', description: 'Quem tinha o link não vê mais o veículo.' });
    },
    onError: (err: Error) => setError(err.message),
  });

  if (!allowed) return null;
  const view = list.data;
  const links = view?.links ?? [];
  const durations = view?.durations ?? [1, 2, 4, 8, 24];
  const full = view ? links.length >= view.maxActive : false;

  return (
    <section className={styles.panel} aria-labelledby={`ao-vivo-${vehicle.id}`}>
      <h2 id={`ao-vivo-${vehicle.id}`} className={styles.title}>
        Localização ao vivo
      </h2>
      <p className={styles.text}>
        Mande um link para alguém acompanhar {vehicle.name} por algumas horas, sem precisar de conta. Ele para sozinho
        no prazo.
      </p>

      {list.isLoading ? (
        <Spinner label="Carregando os links" />
      ) : (
        links.length > 0 && (
          <ul className={styles.links}>
            {links.map((link) => (
              <li key={link.id} className={styles.link}>
                <div className={styles.who}>
                  <strong>{link.label || 'Link ao vivo'}</strong>
                  <span className={styles.muted}>
                    {untilLabel(link.expiresAt)} · {viewsLabel(link.views)}
                  </span>
                </div>
                <div className={styles.row}>
                  <Button variant="secondary" size="small" onClick={() => void share(link)}>
                    Compartilhar
                  </Button>
                  <a
                    className={styles.whatsapp}
                    href={`https://wa.me/?text=${encodeURIComponent(liveShareText(vehicle, link))}`}
                    target="_blank"
                    rel="noreferrer noopener"
                  >
                    WhatsApp
                  </a>
                  <Button
                    variant="ghost"
                    size="small"
                    loading={revoke.isPending && revoke.variables?.id === link.id}
                    onClick={() => revoke.mutate(link)}
                  >
                    Encerrar
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        )
      )}

      {verifying ? (
        create.isPending ? (
          <Spinner label="Gerando o link" />
        ) : (
          <div className={styles.confirm}>
            <IdentityCheck
              purpose="vehicle_share"
              action={`Para mandar a localização de ${vehicle.name}`}
              onGrant={(token) => create.mutate(token)}
            />
            <Button variant="ghost" size="small" onClick={() => setVerifying(false)}>
              Voltar
            </Button>
          </div>
        )
      ) : full ? (
        <p className={styles.muted}>Já são {links.length} links ativos: encerre um para gerar outro.</p>
      ) : (
        <div className={styles.form}>
          <div className={styles.durations} role="radiogroup" aria-label="Por quanto tempo">
            {durations.map((h) => (
              <label key={h} className={`${styles.duration} ${hours === h ? styles.durationOn : ''}`}>
                <input type="radio" name={`ao-vivo-horas-${vehicle.id}`} checked={hours === h} onChange={() => setHours(h)} />
                {durationLabel(h)}
              </label>
            ))}
          </div>
          <TextField
            label="Para quem (opcional)"
            maxLength={40}
            value={label}
            placeholder="Ex.: Mãe, entregador"
            onChange={(e) => setLabel(e.target.value)}
          />
          <Button variant="primary" block onClick={() => setVerifying(true)}>
            Gerar link por {durationLabel(hours)}
          </Button>
        </div>
      )}
      {error && (
        <p className={styles.error} role="alert">
          {error}
        </p>
      )}
    </section>
  );
}

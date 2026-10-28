import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { installersApi } from '@/api/resources';
import type { InstallerInput } from '@/api/resources';
import billing from '@/components/billing/Billing.module.css';
import { InstallersModal } from '@/components/landing/InstallersModal';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { TextField } from '@/components/ui/Field';
import { Modal } from '@/components/ui/Modal';
import { Spinner } from '@/components/ui/Spinner';
import { useToast } from '@/components/ui/Toast';
import { centsToInput, formatMoney, parseMoney } from '@/services/format';
import type { Installer } from '@/types';
import { Pager } from '@/components/ui/Pager';
import { usePagedList } from '@/hooks/usePagination';

import styles from '../Page.module.css';
import { InstallerLinkModal, InstallerLinksCard } from './InstallerLinks';

interface Draft {
  name: string;
  city: string;
  serviceArea: string;
  whatsapp: string;
  servesMoto: boolean;
  servesCar: boolean;
  priceMoto: string;
  priceCar: string;
  description: string;
  active: boolean;
}

const EMPTY: Draft = {
  name: '',
  city: '',
  serviceArea: '',
  whatsapp: '',
  servesMoto: true,
  servesCar: true,
  priceMoto: '',
  priceCar: '',
  description: '',
  active: true,
};

/** Número com 55 → "(11) 99999-0000" para exibir. */
function formatPhone(digits: string): string {
  const local = digits.startsWith('55') ? digits.slice(2) : digits;
  const ddd = local.slice(0, 2);
  const number = local.slice(2);
  const split = number.length === 9 ? 5 : 4;
  return `(${ddd}) ${number.slice(0, split)}-${number.slice(split)}`;
}

function draftFrom(i: Installer): Draft {
  return {
    name: i.name,
    city: i.city,
    serviceArea: i.serviceArea,
    whatsapp: formatPhone(i.whatsapp),
    servesMoto: i.servesMoto,
    servesCar: i.servesCar,
    priceMoto: i.priceMotoCents === null ? '' : centsToInput(i.priceMotoCents),
    priceCar: i.priceCarCents === null ? '' : centsToInput(i.priceCarCents),
    description: i.description,
    active: i.active,
  };
}

/** Converte o formulário no corpo da API, ou devolve a mensagem de erro. */
function inputFrom(d: Draft): InstallerInput | string {
  const price = (text: string) => (text.trim() === '' ? null : parseMoney(text));
  const priceMotoCents = price(d.priceMoto);
  const priceCarCents = price(d.priceCar);
  if ((d.priceMoto.trim() && priceMotoCents === null) || (d.priceCar.trim() && priceCarCents === null)) {
    return 'Valor inválido. Use, por exemplo, 120,00 — ou deixe em branco para "a combinar".';
  }
  return {
    name: d.name,
    city: d.city,
    serviceArea: d.serviceArea,
    whatsapp: d.whatsapp,
    servesMoto: d.servesMoto,
    servesCar: d.servesCar,
    priceMotoCents: d.servesMoto ? priceMotoCents : null,
    priceCarCents: d.servesCar ? priceCarCents : null,
    description: d.description,
    active: d.active,
  };
}

/**
 * Prestadores de instalação recomendados. Os ativos aparecem na landing page
 * e no painel do cliente; a instalação é combinada e paga direto com eles.
 * Daqui também sai o link da instalação avulsa (o rastreador escolhido, sem
 * agendamento): o instalador testa o sinal e conclui.
 */
export function InstallersPage() {
  const { notify } = useToast();
  const queryClient = useQueryClient();

  const [draft, setDraft] = useState<Draft | null>(null);
  const [editing, setEditing] = useState<Installer | null>(null);
  const [removing, setRemoving] = useState<Installer | null>(null);
  const [formError, setFormError] = useState('');
  const [preview, setPreview] = useState(false);
  // O link da instalação avulsa (com o prestador já escolhido, se veio da linha).
  const [linkFor, setLinkFor] = useState<{ installerId: string } | null>(null);

  const installers = useQuery({ queryKey: ['installers'], queryFn: installersApi.list });

  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: ['installers'] });
    queryClient.invalidateQueries({ queryKey: ['public', 'installers'] });
  };

  const save = useMutation({
    mutationFn: (input: InstallerInput) =>
      editing ? installersApi.update(editing.id, input) : installersApi.create(input),
    onSuccess: (saved) => {
      refresh();
      notify({ tone: 'success', title: editing ? 'Prestador atualizado' : 'Prestador cadastrado', description: saved.name });
      setDraft(null);
      setEditing(null);
    },
    onError: (err: Error) => setFormError(err.message),
  });

  const toggle = useMutation({
    mutationFn: (i: Installer) => {
      const input = inputFrom({ ...draftFrom(i), active: !i.active });
      if (typeof input === 'string') throw new Error(input);
      return installersApi.update(i.id, input);
    },
    onSuccess: (saved) => {
      refresh();
      notify({ tone: 'success', title: saved.active ? 'Visível na landing' : 'Oculto da landing', description: saved.name });
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não foi possível alterar', description: err.message }),
  });

  const remove = useMutation({
    mutationFn: (id: string) => installersApi.remove(id),
    onSuccess: () => {
      refresh();
      notify({ tone: 'success', title: 'Prestador excluído' });
      setRemoving(null);
    },
    onError: (err: Error) => notify({ tone: 'error', title: 'Não foi possível excluir', description: err.message }),
  });

  const submit = () => {
    if (!draft) return;
    const input = inputFrom(draft);
    if (typeof input === 'string') return setFormError(input);
    setFormError('');
    save.mutate(input);
  };

  const list = installers.data ?? [];
  const paged = usePagedList(list, 'installers');

  return (
    <div className={styles.page}>
      <div className={styles.inner}>
        <header className={styles.header}>
          <div>
            <h1 className={styles.title}>Prestadores</h1>
            <p className={styles.description}>
              Técnicos recomendados para a instalação. A instalação é combinada e paga direto com
              eles; os ativos aparecem na landing page e no painel do cliente, com o botão de WhatsApp.
            </p>
          </div>
          <div className={styles.actions}>
            <Button variant="secondary" onClick={() => setPreview(true)}>
              Ver como o cliente vê
            </Button>
            <Button variant="secondary" onClick={() => setLinkFor({ installerId: '' })}>
              Instalação avulsa
            </Button>
            <Button
              variant="primary"
              onClick={() => {
                setEditing(null);
                setFormError('');
                setDraft(EMPTY);
              }}
            >
              Novo prestador
            </Button>
          </div>
        </header>

        <Card flush>
          {installers.isLoading ? (
            <Spinner label="Carregando prestadores" />
          ) : list.length === 0 ? (
            <EmptyState
              icon="🔧"
              title="Nenhum prestador cadastrado"
              description="Cadastre os técnicos parceiros para eles aparecerem na landing page como recomendados."
            />
          ) : (
            <>
              <div className={styles.tableWrap}>
                <table className={styles.table}>
                  <thead>
                    <tr>
                      <th>Prestador</th>
                      <th>Atende</th>
                      <th>Valores</th>
                      <th>WhatsApp</th>
                      <th>Situação</th>
                      <th />
                    </tr>
                  </thead>
                  <tbody>
                    {paged.items.map((i) => (
                      <tr key={i.id}>
                        <td>
                          <strong>{i.name}</strong>
                          <div className={billing.muted}>
                            {[i.city, i.serviceArea].filter(Boolean).join(' · ') || '—'}
                          </div>
                        </td>
                        <td>
                          <div style={{ display: 'flex', gap: 'var(--space-1)' }}>
                            {i.servesMoto && <Badge tone="accent">Moto</Badge>}
                            {i.servesCar && <Badge tone="accent">Carro</Badge>}
                          </div>
                        </td>
                        <td className={billing.muted}>
                          {i.servesMoto && <div>Moto: {i.priceMotoCents === null ? 'a combinar' : formatMoney(i.priceMotoCents)}</div>}
                          {i.servesCar && <div>Carro: {i.priceCarCents === null ? 'a combinar' : formatMoney(i.priceCarCents)}</div>}
                        </td>
                        <td>
                          <a href={`https://wa.me/${i.whatsapp}`} target="_blank" rel="noopener noreferrer">
                            {formatPhone(i.whatsapp)}
                          </a>
                        </td>
                        <td>
                          {i.active ? (
                            <Badge tone="success" dot>
                              Na landing
                            </Badge>
                          ) : (
                            <Badge tone="neutral">Oculto</Badge>
                          )}
                        </td>
                        <td>
                          <div className={styles.actions}>
                            <Button size="small" variant="ghost" onClick={() => setLinkFor({ installerId: i.id })}>
                              Mandar link
                            </Button>
                            <Button
                              size="small"
                              variant="ghost"
                              onClick={() => {
                                setEditing(i);
                                setFormError('');
                                setDraft(draftFrom(i));
                              }}
                            >
                              Editar
                            </Button>
                            <Button size="small" variant="ghost" loading={toggle.isPending && toggle.variables?.id === i.id} onClick={() => toggle.mutate(i)}>
                              {i.active ? 'Ocultar' : 'Mostrar'}
                            </Button>
                            <Button size="small" variant="ghost" onClick={() => setRemoving(i)}>
                              Excluir
                            </Button>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <Pager {...paged.pager} />
            </>
          )}
        </Card>

        <InstallerLinksCard installers={list} />
      </div>

      {linkFor && <InstallerLinkModal installers={list} installerId={linkFor.installerId} onClose={() => setLinkFor(null)} />}

      <Modal
        open={draft !== null}
        wide
        title={editing ? 'Editar prestador' : 'Novo prestador'}
        onClose={() => setDraft(null)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setDraft(null)}>
              Cancelar
            </Button>
            <Button variant="primary" loading={save.isPending} disabled={!draft?.name.trim() || !draft?.whatsapp.trim()} onClick={submit}>
              Salvar
            </Button>
          </>
        }
      >
        {draft && (
          <div className={styles.form}>
            {formError && <div className={styles.note}>{formError}</div>}
            <div className={styles.formRow}>
              <TextField
                label="Nome"
                placeholder="Ex.: Auto Elétrica do Zé"
                required
                autoFocus
                value={draft.name}
                onChange={(e) => setDraft({ ...draft, name: e.target.value })}
              />
              <TextField
                label="WhatsApp"
                placeholder="(11) 99999-0000"
                required
                value={draft.whatsapp}
                onChange={(e) => setDraft({ ...draft, whatsapp: e.target.value })}
              />
            </div>
            <div className={styles.formRow}>
              <TextField
                label="Cidade"
                placeholder="São Paulo - SP"
                value={draft.city}
                onChange={(e) => setDraft({ ...draft, city: e.target.value })}
              />
              <TextField
                label="Regiões atendidas"
                placeholder="Zona Sul, ABC, atende a domicílio"
                value={draft.serviceArea}
                onChange={(e) => setDraft({ ...draft, serviceArea: e.target.value })}
              />
            </div>

            <fieldset style={{ border: 0, padding: 0, margin: 0 }}>
              <legend className={styles.infoLabel}>Atende</legend>
              <div style={{ display: 'flex', gap: 'var(--space-5)' }}>
                <label style={{ display: 'flex', gap: 'var(--space-2)', alignItems: 'center' }}>
                  <input type="checkbox" checked={draft.servesMoto} onChange={(e) => setDraft({ ...draft, servesMoto: e.target.checked })} />
                  Moto
                </label>
                <label style={{ display: 'flex', gap: 'var(--space-2)', alignItems: 'center' }}>
                  <input type="checkbox" checked={draft.servesCar} onChange={(e) => setDraft({ ...draft, servesCar: e.target.checked })} />
                  Carro
                </label>
              </div>
            </fieldset>

            <div className={styles.formRow}>
              {draft.servesMoto && (
                <TextField
                  label="Valor da instalação — moto (R$)"
                  inputMode="decimal"
                  placeholder="120,00"
                  hint="Em branco mostra “a combinar”."
                  value={draft.priceMoto}
                  onChange={(e) => setDraft({ ...draft, priceMoto: e.target.value })}
                />
              )}
              {draft.servesCar && (
                <TextField
                  label="Valor da instalação — carro (R$)"
                  inputMode="decimal"
                  placeholder="180,00"
                  hint="Em branco mostra “a combinar”."
                  value={draft.priceCar}
                  onChange={(e) => setDraft({ ...draft, priceCar: e.target.value })}
                />
              )}
            </div>

            <TextField
              label="Descrição curta"
              placeholder="Ex.: 10 anos de experiência com rastreadores, atende também aos sábados."
              hint="Até 300 caracteres; aparece no card do prestador."
              value={draft.description}
              onChange={(e) => setDraft({ ...draft, description: e.target.value })}
            />

            <label style={{ display: 'flex', gap: 'var(--space-2)', alignItems: 'center' }}>
              <input type="checkbox" checked={draft.active} onChange={(e) => setDraft({ ...draft, active: e.target.checked })} />
              Mostrar na landing page e no painel do cliente
            </label>
          </div>
        )}
      </Modal>

      <Modal
        open={removing !== null}
        title="Excluir prestador?"
        onClose={() => setRemoving(null)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setRemoving(null)}>
              Voltar
            </Button>
            <Button variant="danger" loading={remove.isPending} onClick={() => removing && remove.mutate(removing.id)}>
              Excluir
            </Button>
          </>
        }
      >
        <p>
          <strong>{removing?.name}</strong> sai da lista de vez. Para só tirar da landing por um tempo, use
          “Ocultar”.
        </p>
      </Modal>

      <InstallersModal isOpen={preview} onClose={() => setPreview(false)} />
    </div>
  );
}

import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { fleetApi } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import { TextField } from '@/components/ui/Field';
import { Modal } from '@/components/ui/Modal';
import { useToast } from '@/components/ui/Toast';
import { fleetKey } from '@/hooks/useFleet';
import type { FleetSettings } from '@/types';

import styles from './FleetSettingsModal.module.css';

/**
 * Grupos e campos: os grupos de veículos da conta (criar, renomear, apagar —
 * os veículos do grupo apagado ficam sem grupo) e os nomes dos dois campos
 * personalizados (ex.: "Nº do contrato com a prefeitura").
 */
export function FleetSettingsModal({ open, fleet, onClose }: { open: boolean; fleet?: FleetSettings; onClose: () => void }) {
  const queryClient = useQueryClient();
  const { notify } = useToast();
  const [name, setName] = useState('');
  const [renaming, setRenaming] = useState<{ id: string; name: string } | null>(null);
  const [labels, setLabels] = useState(fleet?.labels ?? { label1: '', label2: '' });
  const [error, setError] = useState('');
  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: fleetKey });
    queryClient.invalidateQueries({ queryKey: ['vehicles'] });
  };
  const fail = (err: Error) => setError(err.message);

  const create = useMutation({
    mutationFn: () => fleetApi.createGroup(name),
    onSuccess: () => {
      setName('');
      setError('');
      refresh();
    },
    onError: fail,
  });
  const rename = useMutation({
    mutationFn: (r: { id: string; name: string }) => fleetApi.renameGroup(r.id, r.name),
    onSuccess: () => {
      setRenaming(null);
      setError('');
      refresh();
    },
    onError: fail,
  });
  const remove = useMutation({
    mutationFn: (id: string) => fleetApi.deleteGroup(id),
    onSuccess: refresh,
    onError: fail,
  });
  const saveLabels = useMutation({
    mutationFn: () => fleetApi.setLabels(labels),
    onSuccess: () => {
      setError('');
      refresh();
      notify({ tone: 'success', title: 'Campos salvos' });
    },
    onError: fail,
  });

  const groups = fleet?.groups ?? [];
  return (
    <Modal open={open} title="Grupos e campos" onClose={onClose} footer={<Button variant="ghost" onClick={onClose}>Fechar</Button>}>
      <div className={styles.body}>
        <section className={styles.section}>
          <h3 className={styles.heading}>Grupos de veículos</h3>
          <p className={styles.muted}>
            Separe a frota (ex.: "Entregas Zona Sul", "Diretoria") para filtrar no mapa, nos relatórios e nas cercas. O grupo
            de cada veículo se escolhe no cadastro dele.
          </p>
          {groups.length > 0 && (
            <ul className={styles.groups}>
              {groups.map((g) => (
                <li key={g.id}>
                  {renaming?.id === g.id ? (
                    <>
                      <input
                        className={styles.inline}
                        value={renaming.name}
                        maxLength={60}
                        aria-label="Novo nome do grupo"
                        autoFocus
                        onChange={(e) => setRenaming({ id: g.id, name: e.target.value })}
                        onKeyDown={(e) => e.key === 'Enter' && rename.mutate(renaming)}
                      />
                      <Button size="small" variant="primary" loading={rename.isPending} onClick={() => rename.mutate(renaming)}>
                        Salvar
                      </Button>
                      <Button size="small" variant="ghost" onClick={() => setRenaming(null)}>
                        Cancelar
                      </Button>
                    </>
                  ) : (
                    <>
                      <span className={styles.groupName}>
                        <strong>{g.name}</strong>
                        <span className={styles.muted}>
                          {g.vehicles} {g.vehicles === 1 ? 'veículo' : 'veículos'}
                        </span>
                      </span>
                      <Button size="small" variant="ghost" onClick={() => setRenaming({ id: g.id, name: g.name })}>
                        Renomear
                      </Button>
                      <Button size="small" variant="ghost" loading={remove.isPending && remove.variables === g.id} onClick={() => remove.mutate(g.id)}>
                        Apagar
                      </Button>
                    </>
                  )}
                </li>
              ))}
            </ul>
          )}
          {groups.length < (fleet?.maxGroups ?? 100) && (
            <div className={styles.add}>
              <input
                className={styles.inline}
                placeholder="Nome do grupo novo"
                aria-label="Nome do grupo novo"
                maxLength={60}
                value={name}
                onChange={(e) => setName(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && name.trim() && create.mutate()}
              />
              <Button variant="secondary" loading={create.isPending} disabled={!name.trim()} onClick={() => create.mutate()}>
                Criar grupo
              </Button>
            </div>
          )}
        </section>

        <section className={styles.section}>
          <h3 className={styles.heading}>Campos personalizados</h3>
          <p className={styles.muted}>
            Dois campos a mais no cadastro de cada veículo, com o nome que você der — por exemplo, o nº do contrato com a
            prefeitura ou o centro de custo. Dá para buscar a frota por eles.
          </p>
          <div className={styles.labels}>
            <TextField
              label="Nome do campo 1"
              placeholder="Ex.: Nº do contrato"
              maxLength={fleet?.maxLabelLength ?? 40}
              value={labels.label1}
              onChange={(e) => setLabels({ ...labels, label1: e.target.value })}
            />
            <TextField
              label="Nome do campo 2"
              placeholder="Ex.: Centro de custo"
              maxLength={fleet?.maxLabelLength ?? 40}
              value={labels.label2}
              onChange={(e) => setLabels({ ...labels, label2: e.target.value })}
            />
          </div>
          <div>
            <Button variant="secondary" loading={saveLabels.isPending} onClick={() => saveLabels.mutate()}>
              Salvar os nomes
            </Button>
          </div>
        </section>
        {error && (
          <p className={styles.error} role="alert">
            {error}
          </p>
        )}
      </div>
    </Modal>
  );
}

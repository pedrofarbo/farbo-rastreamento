import { useState } from 'react';
import type { FormEvent } from 'react';
import { useQueryClient } from '@tanstack/react-query';

import { commandsApi } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import { TextField } from '@/components/ui/Field';
import { useRealtimeEvent } from '@/hooks/useRealtime';
import { useAuth } from '@/stores/AuthContext';
import type { DeviceCommand, VehicleView } from '@/types';

import styles from './CommandPanel.module.css';

/**
 * O comando livre (só o administrador): o texto vai cru ao rastreador — para
 * configurar o aparelho (ex.: os SZCS# do J16) ou consultar (PARAM#,
 * STATUS#). A resposta chega pelo WebSocket e fica no histórico de comandos.
 */
export function CustomCommand({ vehicle }: { vehicle: VehicleView }) {
  const { canManage } = useAuth();
  const queryClient = useQueryClient();
  const [raw, setRaw] = useState('');
  const [sending, setSending] = useState(false);
  const [command, setCommand] = useState<DeviceCommand | null>(null);
  const [error, setError] = useState('');

  useRealtimeEvent(['command.acknowledged', 'command.failed'], (message) => {
    const updated = message.data as DeviceCommand | undefined;
    if (!updated || !command || updated.id !== command.id) return;
    setCommand(updated);
    void queryClient.invalidateQueries({ queryKey: ['commands', vehicle.id] });
  });

  if (!canManage || !vehicle.device) return null;

  const send = async (event: FormEvent) => {
    event.preventDefault();
    const text = raw.trim();
    if (!text || sending) return;
    setSending(true);
    setError('');
    setCommand(null);
    try {
      const result = await commandsApi.generic(vehicle.id, { command: 'CUSTOM', raw: text });
      setCommand(result);
      void queryClient.invalidateQueries({ queryKey: ['commands', vehicle.id] });
    } catch (err) {
      setError(err instanceof Error && err.message ? err.message : 'Não deu para enviar agora.');
    } finally {
      setSending(false);
    }
  };

  const status =
    command?.status === 'ACKNOWLEDGED'
      ? `Resposta do rastreador: ${command.response || '(sem texto)'}`
      : command?.status === 'FAILED' || command?.status === 'TIMEOUT'
        ? `Não confirmado: ${command.error || 'sem resposta do rastreador'}`
        : command
          ? 'Enviado; esperando a resposta do rastreador…'
          : '';

  return (
    <form className={styles.custom} onSubmit={send}>
      <TextField
        label="Comando livre (administrador)"
        placeholder="Ex.: PARAM#"
        autoCapitalize="characters"
        autoComplete="off"
        spellCheck={false}
        hint="O texto vai como está ao rastreador. A resposta fica no histórico de comandos."
        value={raw}
        onChange={(e) => setRaw(e.target.value)}
      />
      <Button type="submit" variant="secondary" loading={sending} disabled={!raw.trim()}>
        Enviar comando
      </Button>
      {(status || error) && (
        <div className={styles.notice} role="status">
          {error || status}
        </div>
      )}
    </form>
  );
}

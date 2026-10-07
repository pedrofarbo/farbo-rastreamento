import { useEffect, useMemo, useState } from 'react';

import billing from '@/components/billing/Billing.module.css';
import { Badge } from '@/components/ui/Badge';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import fieldStyles from '@/components/ui/Field.module.css';
import pageStyles from '@/pages/Page.module.css';
import { ALERT_STATUS, alertKindLabel, isClock, sameKinds } from '@/services/alerts';
import { formatDateTime } from '@/services/format';
import type { AlertKind, AlertKindInfo, AlertSettings, AlertSettingsInput } from '@/types';

import styles from './AlertSettingsPanel.module.css';

interface AlertSettingsPanelProps {
  settings: AlertSettings;
  /** Quem está vendo: o próprio cliente ou a central na ficha dele. */
  audience: 'customer' | 'central';
  saving: boolean;
  onSave: (input: AlertSettingsInput) => void;
  /** Só o cliente manda e-mail de teste para si mesmo. */
  onTest?: () => void;
  testing?: boolean;
}

/**
 * Escolhas de alertas por e-mail e o histórico do que foi enviado. Os tipos
 * vêm do servidor (catálogo), para a tela e o motor nunca divergirem.
 */
export function AlertSettingsPanel({ settings, audience, saving, onSave, onTest, testing }: AlertSettingsPanelProps) {
  const [kinds, setKinds] = useState<AlertKind[]>(settings.kinds);
  const [guardStart, setGuardStart] = useState(settings.guardStart);
  const [guardEnd, setGuardEnd] = useState(settings.guardEnd);

  // Recarregou do servidor (salvou, trocou de cliente): volta ao que está gravado.
  useEffect(() => {
    setKinds(settings.kinds);
    setGuardStart(settings.guardStart);
    setGuardEnd(settings.guardEnd);
  }, [settings]);

  const security = settings.catalog.filter((info) => info.security);
  const others = settings.catalog.filter((info) => !info.security);

  const clockError = useMemo(() => {
    if (!isClock(guardStart) || !isClock(guardEnd)) return 'Use horários no formato HH:MM.';
    if (guardStart.trim() === guardEnd.trim()) return 'O horário de vigilância precisa ter início e fim diferentes.';
    return '';
  }, [guardStart, guardEnd]);

  const dirty =
    !sameKinds(kinds, settings.kinds) || guardStart !== settings.guardStart || guardEnd !== settings.guardEnd;

  const toggle = (kind: AlertKind) =>
    setKinds((current) => (current.includes(kind) ? current.filter((k) => k !== kind) : [...current, kind]));

  const own = audience === 'customer';

  const renderItem = (info: AlertKindInfo) => {
    const checked = kinds.includes(info.kind);
    const id = `alert-${info.kind}`;
    return (
      <li key={info.kind} className={styles.item}>
        <input
          id={id}
          type="checkbox"
          className={styles.switch}
          checked={checked}
          onChange={() => toggle(info.kind)}
          disabled={saving}
        />
        <div className={styles.itemText}>
          <label htmlFor={id} className={styles.itemLabel}>
            {info.label}
          </label>
          <p className={styles.itemDescription}>{info.description}</p>
          {info.kind === 'IGNITION_GUARD' && (
            <div className={styles.guard}>
              <span>das</span>
              <input
                type="time"
                aria-label="Início do horário de vigilância"
                className={`${fieldStyles.input} ${styles.clock}`}
                value={guardStart}
                onChange={(event) => setGuardStart(event.target.value)}
                disabled={saving}
              />
              <span>às</span>
              <input
                type="time"
                aria-label="Fim do horário de vigilância"
                className={`${fieldStyles.input} ${styles.clock}`}
                value={guardEnd}
                onChange={(event) => setGuardEnd(event.target.value)}
                disabled={saving}
              />
            </div>
          )}
        </div>
      </li>
    );
  };

  return (
    <div className={styles.stack}>
      {!settings.enabled && (
        <div className={`${billing.banner} ${billing.bannerWarning}`} role="status">
          <span>Os alertas por e-mail estão desligados no servidor: as escolhas ficam guardadas, mas nada é enviado.</span>
        </div>
      )}
      {settings.enabled && !settings.mailConfigured && (
        <div className={`${billing.banner} ${billing.bannerWarning}`} role="status">
          <span>
            {own
              ? 'O envio de e-mails ainda não está ativo: os alertas começam a chegar assim que ele for ligado.'
              : 'SMTP não configurado (SMTP_HOST): nenhum alerta sai até ele ser configurado.'}
          </span>
        </div>
      )}
      {settings.suspended && (
        <div className={`${billing.banner} ${billing.bannerDanger}`} role="alert">
          <span>
            {own
              ? 'Com o acesso suspenso, seus alertas ficam pausados até o pagamento ser confirmado.'
              : 'Cliente suspenso: os alertas dele estão pausados até o pagamento ser confirmado. Os alertas de segurança continuam indo para a central.'}
          </span>
        </div>
      )}

      <Card
        title="Quais alertas receber"
        subtitle={
          <>
            {own ? 'Os alertas chegam em ' : 'Os alertas vão para '}
            <strong>{settings.email}</strong>.{' '}
            {settings.custom ? '' : 'Ainda valem as escolhas padrão.'}
          </>
        }
      >
        <h3 className={styles.groupTitle}>Segurança</h3>
        <ul className={styles.list}>{security.map(renderItem)}</ul>
        <h3 className={styles.groupTitle}>Outros alertas</h3>
        <ul className={styles.list}>{others.map(renderItem)}</ul>

        <p className={styles.note}>
          Para não lotar a caixa de entrada, o mesmo alerta do mesmo veículo sai no máximo uma vez a cada{' '}
          {settings.cooldownMinutes} minutos; os repetidos entram como contagem no próximo e-mail. O que o
          rastreador guardou enquanto estava sem sinal e mandou depois não gera e-mail.
        </p>

        {clockError && <p className={fieldStyles.error}>{clockError}</p>}
        <div className={styles.footer}>
          {onTest && (
            <Button variant="secondary" onClick={onTest} loading={testing} disabled={saving}>
              Enviar e-mail de teste
            </Button>
          )}
          <Button
            onClick={() => onSave({ kinds, guardStart: guardStart.trim(), guardEnd: guardEnd.trim() })}
            loading={saving}
            disabled={!dirty || Boolean(clockError)}
          >
            Salvar alertas
          </Button>
        </div>
      </Card>

      <Card
        title="Últimos alertas"
        subtitle={own ? 'Os e-mails de alerta enviados para você.' : 'Os e-mails de alerta enviados ao cliente.'}
        flush
      >
        {settings.history.length === 0 ? (
          <p className={styles.empty}>Nenhum alerta enviado ainda.</p>
        ) : (
          <div className={styles.tableWrap}>
            <table className={`${styles.table} ${pageStyles.stackTable}`}>
              <thead>
                <tr>
                  <th>Alerta</th>
                  <th>Quando</th>
                  <th>Veículo</th>
                  <th>Situação</th>
                </tr>
              </thead>
              <tbody>
                {settings.history.map((n) => {
                  const status = ALERT_STATUS[n.status];
                  return (
                    <tr key={n.id}>
                      <td>
                        {alertKindLabel(n.kind, settings.catalog)}
                        {n.detail && <> {n.detail}</>}
                        {n.suppressedCount > 0 && (
                          <span className={styles.repeats}>
                            {' '}
                            +{n.suppressedCount} {n.suppressedCount === 1 ? 'repetido' : 'repetidos'}
                          </span>
                        )}
                      </td>
                      <td data-label="Quando">{formatDateTime(n.occurredAt)}</td>
                      <td data-label="Veículo">
                        <span>
                          {n.vehicleName || '—'}
                          {n.plate && <span className={styles.plate}> {n.plate}</span>}
                        </span>
                      </td>
                      <td data-label="Situação">
                        <span>
                          <Badge tone={status.tone}>{status.label}</Badge>
                          {n.pushSent > 0 && <span className={styles.repeats}> e no celular</span>}
                        </span>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </Card>
    </div>
  );
}

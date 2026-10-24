import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';

import { orgApi } from '@/api/resources';
import { AppointmentList } from '@/components/install/AppointmentList';
import styles from '@/components/install/Appointments.module.css';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { Spinner } from '@/components/ui/Spinner';
import { formatDateOnly, todayISO } from '@/services/format';
import { dayKey } from '@/services/installations';

import pageStyles from '../Page.module.css';

function addDays(day: string, n: number): string {
  const [y, m, d] = day.split('-').map(Number);
  return dayKey(new Date(y, m - 1, d + n));
}

/** As instalações e as retiradas agendadas pela Farbo nos veículos dos associados (a semana). */
export function OrgInstallationsPage() {
  const [from, setFrom] = useState(todayISO());
  const to = addDays(from, 6);
  const list = useQuery({ queryKey: ['org', 'installations', from], queryFn: () => orgApi.installations(from, to), refetchInterval: 60_000 });
  return (
    <div className={pageStyles.page}>
      <div className={pageStyles.inner}>
        <header className={pageStyles.header}>
          <div>
            <h1 className={pageStyles.title}>Instalações</h1>
            <p className={pageStyles.description}>
              Quando o instalador vai a cada associado, para instalar ou retirar o rastreador, e o que já foi feito. A
              Farbo agenda com o associado, que recebe o aviso no app. Para pedir a retirada, abra o associado.
            </p>
          </div>
        </header>
        <Card
          title="Agenda"
          actions={
            <div className={styles.toolbar}>
              <Button size="small" variant="ghost" aria-label="Semana anterior" onClick={() => setFrom(addDays(from, -7))}>
                ‹
              </Button>
              <span className={styles.period}>
                {formatDateOnly(from).slice(0, 5)} a {formatDateOnly(to).slice(0, 5)}
              </span>
              <Button size="small" variant="ghost" aria-label="Próxima semana" onClick={() => setFrom(addDays(from, 7))}>
                ›
              </Button>
            </div>
          }
          flush
        >
          {list.isLoading ? <Spinner label="Carregando" /> : <AppointmentList list={list.data ?? []} />}
        </Card>
      </div>
    </div>
  );
}

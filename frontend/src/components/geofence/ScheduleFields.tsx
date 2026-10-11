import { DAY_NAMES, minutesToClock, clockToMinutes } from '@/app/fence';

import styles from './ScheduleFields.module.css';

export interface Schedule {
  scheduleDays: number[];
  scheduleStart: number | null;
  scheduleEnd: number | null;
}

/**
 * O horário dos avisos de uma cerca: sempre, ou só em alguns dias e num
 * intervalo (que pode atravessar a meia-noite: das 22:00 às 06:00). A entrada
 * e a saída ficam no histórico a qualquer hora.
 */
export function ScheduleFields({ value, onChange }: { value: Schedule; onChange: (next: Schedule) => void }) {
  const limited = value.scheduleDays.length > 0 || value.scheduleStart !== null;
  const hours = value.scheduleStart !== null && value.scheduleEnd !== null;
  const toggleDay = (day: number) =>
    onChange({
      ...value,
      scheduleDays: value.scheduleDays.includes(day)
        ? value.scheduleDays.filter((d) => d !== day)
        : [...value.scheduleDays, day].sort((a, b) => a - b),
    });

  return (
    <div className={styles.schedule}>
      <div className={styles.modes} role="radiogroup" aria-label="Quando avisar">
        <button
          type="button"
          role="radio"
          aria-checked={!limited}
          className={!limited ? styles.on : styles.off}
          onClick={() => onChange({ scheduleDays: [], scheduleStart: null, scheduleEnd: null })}
        >
          Sempre
        </button>
        <button
          type="button"
          role="radio"
          aria-checked={limited}
          className={limited ? styles.on : styles.off}
          onClick={() =>
            limited || onChange({ scheduleDays: [1, 2, 3, 4, 5], scheduleStart: 8 * 60, scheduleEnd: 18 * 60 })
          }
        >
          Só em alguns horários
        </button>
      </div>
      {limited && (
        <>
          <div className={styles.days} role="group" aria-label="Dias">
            {DAY_NAMES.map((name, day) => (
              <button
                key={name}
                type="button"
                aria-pressed={value.scheduleDays.includes(day)}
                className={value.scheduleDays.includes(day) ? styles.dayOn : styles.dayOff}
                onClick={() => toggleDay(day)}
              >
                {name}
              </button>
            ))}
          </div>
          <label className={styles.allDay}>
            <input
              type="checkbox"
              checked={!hours}
              onChange={() =>
                onChange(
                  hours
                    ? { ...value, scheduleStart: null, scheduleEnd: null }
                    : { ...value, scheduleStart: 8 * 60, scheduleEnd: 18 * 60 },
                )
              }
            />
            O dia todo
          </label>
          {hours && (
            <div className={styles.hours}>
              <label>
                Das
                <input
                  type="time"
                  value={minutesToClock(value.scheduleStart!)}
                  onChange={(e) => {
                    const m = clockToMinutes(e.target.value);
                    if (m !== null) onChange({ ...value, scheduleStart: m });
                  }}
                />
              </label>
              <label>
                Às
                <input
                  type="time"
                  value={minutesToClock(value.scheduleEnd!)}
                  onChange={(e) => {
                    const m = clockToMinutes(e.target.value);
                    if (m !== null) onChange({ ...value, scheduleEnd: m });
                  }}
                />
              </label>
            </div>
          )}
          <p className={styles.hint}>
            {value.scheduleDays.length === 0 ? 'Sem dia marcado, vale para todos. ' : ''}
            Um horário como das 22:00 às 06:00 atravessa a meia-noite. Fora dele, a entrada e a saída ficam só no
            histórico.
          </p>
        </>
      )}
    </div>
  );
}

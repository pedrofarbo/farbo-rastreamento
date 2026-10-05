import { describe, expect, it } from 'vitest';

import { byOrigin, eventLabel, waitlistCsv } from './WaitlistTab';

describe('waitlistCsv', () => {
  it('abre no Excel em português: BOM, ";" e aspas escapadas', () => {
    const csv = waitlistCsv([
      {
        id: '1', name: 'João "Jota" Silva', email: 'joao@exemplo.com.br', phone: '(11) 98888-7777', consentAt: '2026-10-03T12:00:00Z',
        createdAt: '2026-10-03T12:00:00Z', updatedAt: '2026-10-03T12:00:00Z', customerId: null, promoClaimed: false,
        event: 'encontro-insanos-mc',
        city: 'São Paulo - SP',
      },
      {
        id: '2', name: '', email: 'ana@exemplo.com', phone: '', consentAt: '2026-10-02T09:30:00Z',
        createdAt: '2026-10-02T09:30:00Z', updatedAt: '2026-10-02T09:30:00Z', customerId: null, promoClaimed: false,
        event: '',
        city: '',
      },
    ]);
    expect(csv.startsWith('\uFEFFNome;E-mail;WhatsApp;Cidade da instalação;Origem;Inscrito em\r\n')).toBe(true);
    const lines = csv.slice(1).split('\r\n');
    expect(lines).toHaveLength(3);
    expect(lines[1].startsWith('"João ""Jota"" Silva";"joao@exemplo.com.br";"(11) 98888-7777";"São Paulo - SP";"Encontro Insanos Mc";')).toBe(true);
    expect(lines[2].startsWith('"";"ana@exemplo.com";"";"";"Site";')).toBe(true);
  });
});

describe('origem', () => {
  it('nome do evento e a contagem por origem', () => {
    expect(eventLabel('')).toBe('Site');
    expect(eventLabel('feira-de-motos-sp')).toBe('Feira De Motos Sp');
    const entry = (event: string) => ({ event }) as Parameters<typeof byOrigin>[0][number];
    expect(byOrigin([entry('a'), entry(''), entry('a'), entry('b')])).toEqual([
      { label: 'A', count: 2 },
      { label: 'Site', count: 1 },
      { label: 'B', count: 1 },
    ]);
  });
});

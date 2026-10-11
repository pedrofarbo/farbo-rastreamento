import { describe, expect, it } from 'vitest';

import type { FleetSettings } from '@/types';
import { MAX_IMPORT_ROWS, parseCsv, parseFleetSheet, templateCsv } from './fleetImport';

const fleet: FleetSettings = {
  groups: [],
  labels: { label1: 'Nº do contrato', label2: '' },
  maxGroups: 100,
  maxCustomLength: 100,
  maxLabelLength: 40,
};

describe('parseCsv', () => {
  it('lê o CSV do Excel em português: BOM, ";" e aspas', () => {
    expect(parseCsv('﻿Nome;Placa\r\n"Van; 01";ABC1D23\r\n"Diz ""oi""";\r\n')).toEqual([
      ['Nome', 'Placa'],
      ['Van; 01', 'ABC1D23'],
      ['Diz "oi"', ''],
    ]);
  });

  it('descobre a vírgula e a tabulação como separador', () => {
    expect(parseCsv('Nome,Placa\nVan,ABC')).toEqual([
      ['Nome', 'Placa'],
      ['Van', 'ABC'],
    ]);
    expect(parseCsv('Nome\tPlaca\nVan\tABC')).toEqual([
      ['Nome', 'Placa'],
      ['Van', 'ABC'],
    ]);
  });

  it('aceita quebra de linha dentro das aspas', () => {
    expect(parseCsv('Nome;Cor\n"Van\n01";Branca')).toEqual([
      ['Nome', 'Cor'],
      ['Van\n01', 'Branca'],
    ]);
  });
});

describe('parseFleetSheet', () => {
  it('acha as colunas pelo nome (sem acento, em qualquer ordem) e monta as linhas', () => {
    const sheet = parseFleetSheet(
      [
        'IMEI do rastreador;veiculo;PLACA;tipo;Ano;grupo;N do Contrato;Observação',
        '8692 4706 1900 001;Van 01;abc1d23;Moto;2022;Zona Sul;PMSP-7;qualquer coisa',
        ';;;;;;;',
        ';Van 02;;carro;;;;',
      ].join('\n'),
      fleet,
    );
    expect(sheet.problems).toEqual([]);
    expect(sheet.ignored).toEqual(['Observação']);
    expect(sheet.lines).toEqual([2, 4]);
    expect(sheet.rows).toEqual([
      {
        vehicle: { name: 'Van 01', plate: 'ABC1D23', kind: 'MOTORCYCLE', year: 2022, custom1: 'PMSP-7' },
        group: 'Zona Sul',
        imei: '869247061900001',
      },
      { vehicle: { name: 'Van 02', kind: 'CAR' }, group: '', imei: '' },
    ]);
  });

  it('aponta cada linha com dado errado', () => {
    const sheet = parseFleetSheet('Nome;Tipo;Ano;IMEI\n;caminhão;1800;abc\nVan;;2020;', fleet);
    expect(sheet.problems.map((p) => p.line)).toEqual([2, 2, 2, 2]);
    expect(sheet.problems[0].message).toContain('nome');
    expect(sheet.rows).toHaveLength(2);
  });

  it('recusa a planilha sem a coluna Nome, vazia ou grande demais', () => {
    expect(parseFleetSheet('Placa\nABC', fleet).problems[0].message).toContain('"Nome"');
    expect(parseFleetSheet('', fleet).problems[0].message).toContain('vazia');
    expect(parseFleetSheet('Nome\n', fleet).problems[0].message).toContain('só tem o cabeçalho');
    const big = 'Nome\n' + Array.from({ length: MAX_IMPORT_ROWS + 1 }, (_, i) => `Van ${i}`).join('\n');
    expect(parseFleetSheet(big, fleet).problems[0].message).toContain(`no máximo ${MAX_IMPORT_ROWS}`);
  });

  it('o modelo traz o nome que a conta deu aos campos e volta a ser lido', () => {
    const template = templateCsv(fleet);
    expect(template.startsWith('﻿Nome;Placa;Tipo;')).toBe(true);
    expect(template).toContain(';Nº do contrato;Campo personalizado 2;IMEI do rastreador');
    const sheet = parseFleetSheet(template + 'Van;;;;;;;;C-1;C-2;\n', fleet);
    expect(sheet.problems).toEqual([]);
    expect(sheet.rows[0].vehicle).toEqual({ name: 'Van', custom1: 'C-1', custom2: 'C-2' });
  });
});

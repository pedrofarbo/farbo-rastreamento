import { describe, expect, it } from 'vitest';

import { associateTemplateCsv, parseAssociateSheet } from './associateImport';

describe('planilha de associados', () => {
  it('lê o modelo do Excel (com ";" e BOM)', () => {
    const csv =
      associateTemplateCsv() +
      'Rui Segurado;RUI@Segurado.test;11988887777;529.982.247-25;Gol do Rui;abc1d23;carro;VW;Gol;2019;Prata\r\n' +
      ';;;;;;;;;;\r\n' +
      'Rui Segurado;rui@segurado.test;;;;XYZ9Z99;moto;;;;\r\n' +
      'Lia Segurada;lia@segurado.test;;;;;;;;;\r\n';
    const parsed = parseAssociateSheet(csv);
    expect(parsed.problems).toEqual([]);
    expect(parsed.associates).toBe(2);
    expect(parsed.vehicles).toBe(2);
    expect(parsed.rows[0]).toEqual({
      line: 2,
      name: 'Rui Segurado',
      email: 'rui@segurado.test',
      phone: '11988887777',
      document: '529.982.247-25',
      vehicle: { name: 'Gol do Rui', plate: 'ABC1D23', kind: 'CAR', brand: 'VW', model: 'Gol', year: 2019, color: 'Prata' },
    });
    // Sem nome do veículo, vale a placa; a linha em branco não conta.
    expect(parsed.rows[1]).toMatchObject({ line: 4, vehicle: { name: 'XYZ9Z99', plate: 'XYZ9Z99', kind: 'MOTORCYCLE' } });
    expect(parsed.rows[2].vehicle).toBeUndefined();
  });

  it('aponta o que falta, com a linha', () => {
    expect(parseAssociateSheet('Nome;Placa\nRui;ABC1D23\n').problems[0].message).toContain('"E-mail"');
    const parsed = parseAssociateSheet('Nome,E-mail,Ano,Tipo,Placa\n,rui@x.test,,,\nLia,lia,1800,trator,AAA1A11\n');
    expect(parsed.problems.map((p) => [p.line, p.message])).toEqual([
      [2, 'Falta o nome do associado.'],
      [3, 'E-mail "lia" inválido.'],
      [3, 'Tipo "trator": use carro ou moto.'],
      [3, expect.stringContaining('Ano "1800"')],
    ]);
  });
});

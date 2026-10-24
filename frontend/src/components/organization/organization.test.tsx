// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it } from 'vitest';

import { matchesAssociate } from '@/pages/org/OrgAssociatesPage';
import type { Associate, OrgInvoice } from '@/types';

import { associateProblem } from './OrgForms';
import { itemLabel, OrgInvoices } from './OrgInvoices';
import { organizationFromDraft } from './OrganizationForm';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const text = () => (document.body.textContent ?? '').replace(/ /g, ' ');

afterEach(() => {
  document.body.innerHTML = '';
});

const draft = {
  kind: 'association' as const, name: 'Associação Motoclube', document: '11.222.333/0001-81', email: 'contato@moto.test',
  phone: '', price: '49,90', dueDay: '10', equipmentMode: 'loan' as const, equipment: '', installFee: '80', uninstallFee: '',
  notes: '', active: true, adminName: '', adminEmail: '',
};

describe('o cadastro da organização', () => {
  it('converte os valores e confere o que falta', () => {
    const ok = organizationFromDraft(draft, true);
    expect(ok).toEqual({
      input: expect.objectContaining({ priceCents: 4990, installFeeCents: 8000, uninstallFeeCents: 0, equipmentCents: 0, document: '11222333000181' }),
    });
    expect(organizationFromDraft({ ...draft, email: 'x' }, true)).toMatch(/e-mail de cobrança/);
    expect(organizationFromDraft({ ...draft, document: '11.222.333/0001-00' }, true)).toMatch(/CNPJ/);
    expect(organizationFromDraft({ ...draft, equipmentMode: 'sale' }, true)).toMatch(/preço do rastreador/);
    expect(organizationFromDraft({ ...draft, adminName: 'Ana' }, true)).toMatch(/Administrador/);
    expect(organizationFromDraft({ ...draft, adminName: 'Ana', adminEmail: 'ana@moto.test' }, true)).toEqual(
      expect.objectContaining({ admin: { name: 'Ana', email: 'ana@moto.test' } }),
    );
    // Na edição, a situação vai junto.
    expect(organizationFromDraft({ ...draft, active: false }, false)).toEqual({ input: expect.objectContaining({ active: false }) });
  });

  it('o associado precisa de e-mail (no cadastro) e de um CPF válido, se informado', () => {
    expect(associateProblem({ name: 'Bia', email: 'bia@x.test', phone: '', document: '' }, false)).toBeNull();
    expect(associateProblem({ name: 'Bia', email: '', phone: '', document: '' }, false)).toMatch(/e-mail/);
    expect(associateProblem({ name: 'Bia', email: '', phone: '', document: '' }, true)).toBeNull();
    expect(associateProblem({ name: 'Bia', email: 'bia@x.test', phone: '', document: '111.111.111-11' }, false)).toMatch(/CPF/);
  });

  it('a busca de associados acha pelo nome, e-mail, celular e documento', () => {
    const a = { name: 'Bia Souza', email: 'bia@x.test', phone: '(11) 98888-7777', document: '52998224725' } as Associate;
    expect(matchesAssociate(a, 'souza')).toBe(true);
    expect(matchesAssociate(a, '98888')).toBe(true);
    expect(matchesAssociate(a, '529.982')).toBe(true);
    expect(matchesAssociate(a, 'carlos')).toBe(false);
  });
});

describe('a fatura da organização', () => {
  it('mostra o extrato: mensalidade, proporcional, taxas', async () => {
    const inv = {
      id: 'f1', customerId: 'o1', subscriptionId: null, description: 'Rastreamento — novembro/2026 · 1 veículo',
      amountCents: 14583, dueDate: '2026-11-10', status: 'OPEN', overdue: false, daysOverdue: 0, paidAt: null, paidVia: '',
      paidChargeId: null, paymentUrl: '', pixCode: '', createdAt: '', updatedAt: '', vehicles: 1, priceCents: 5000,
      items: [
        { kind: 'monthly', vehicleId: 'v1', name: 'CB 500', plate: 'ORG1A23', ownerName: 'Bia', amountCents: 5000 },
        { kind: 'prorata', vehicleId: 'v1', name: 'CB 500', plate: 'ORG1A23', ownerName: 'Bia', amountCents: 1583, days: 19 },
        { kind: 'install', vehicleId: 'v1', name: 'CB 500', plate: 'ORG1A23', ownerName: 'Bia', amountCents: 8000 },
      ],
    } as OrgInvoice;
    expect(itemLabel(inv.items[1])).toBe('Proporcional de 19 dias: CB 500 · ORG1A23 — Bia');
    const host = document.createElement('div');
    document.body.appendChild(host);
    let paid: OrgInvoice | null = null;
    await act(async () => createRoot(host).render(<OrgInvoices invoices={[inv]} onPay={(i) => (paid = i)} />));
    expect(text()).toContain('Vence em 10/11/2026');
    expect(text()).not.toContain('Mensalidade: CB 500');
    const toggle = Array.from(host.querySelectorAll('button')).find((b) => b.textContent?.includes('Ver o extrato'))!;
    expect(toggle.textContent).toContain('3 linhas');
    await act(async () => toggle.click());
    expect(text()).toContain('Mensalidade: CB 500 · ORG1A23 — Bia');
    expect(text()).toContain('Instalação: CB 500');
    expect(text()).toContain('Total');
    await act(async () => Array.from(host.querySelectorAll('button')).find((b) => b.textContent === 'Pagar')!.click());
    expect(paid).toBe(inv);
  });
});

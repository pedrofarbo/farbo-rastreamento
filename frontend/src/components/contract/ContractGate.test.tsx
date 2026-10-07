// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { ContractStatus } from '@/types';

let required = true;
let isCustomer = true;
let previous: ContractStatus['previous'] = null;
const accepted: unknown[] = [];
const status = (): ContractStatus => ({
  contract: {
    version: '1', effectiveDate: '7 de outubro de 2026', title: 'Contrato de Prestação de Serviços de Rastreamento Veicular',
    intro: [{ kind: 'p', text: 'Termo de adesão, versão 1.' }],
    sections: [{ title: '6. Permanência mínima e multa', blocks: [{ kind: 'ul', items: ['3 meses', '1 mensalidade'] }] }],
    sha256: 'x',
    changes: 'Entrou o reajuste anual da mensalidade (cláusula 5).',
  },
  required, accepted: required ? null : ({ version: '1' } as ContractStatus['accepted']), previous, name: 'Lia Martins', taxId: '',
});

vi.mock('@/api/resources', () => ({
  contractApi: {
    mine: async () => status(),
    accept: async (version: string, document: string) => {
      if (document.replace(/\D/g, '') !== '52998224725') {
        const { ApiError } = await import('@/api/client');
        throw new ApiError(400, 'CPF inválido');
      }
      accepted.push([version, document]);
      required = false;
      return status();
    },
  },
}));
vi.mock('@/stores/AuthContext', () => ({ useAuth: () => ({ isCustomer, logout: async () => undefined }) }));

import { CONTRACT_REQUIRED_EVENT } from '@/api/client';

import { ContractGate } from './ContractGate';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const flush = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
const text = () => document.body.textContent ?? '';
const button = (label: string) =>
  Array.from(document.querySelectorAll('button')).find((b) => b.textContent === label) as HTMLButtonElement;
function type(input: HTMLInputElement, value: string) {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set?.call(input, value);
  input.dispatchEvent(new Event('input', { bubbles: true }));
}

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <ContractGate>
          <p>o painel</p>
        </ContractGate>
      </QueryClientProvider>,
    ),
  );
  await flush();
}

afterEach(() => {
  document.body.innerHTML = '';
  accepted.length = 0;
  required = true;
  isCustomer = true;
  previous = null;
});

describe('o contrato no primeiro acesso', () => {
  it('segura o cliente até o aceite, com o CPF', async () => {
    await render();
    expect(text()).not.toContain('o painel');
    expect(text()).toContain('Lia, antes de continuar');
    expect(text()).toContain('Os pontos principais');
    expect(text()).toContain('O rastreador é seu.');
    expect(text()).toContain('É só o sistema de rastreamento');
    expect(text()).toContain('não garantimos a recuperação do veículo');
    expect(text()).toContain('Permanência mínima de 3 meses');
    expect(text()).toContain('órgãos de proteção ao crédito');
    expect(text()).toContain('plano de telecomunicações M2M');
    expect(text()).toContain('6. Permanência mínima e multa');
    expect(text()).toContain('NF-e do rastreador e a NFS-e das mensalidades');
    expect(text()).toContain('Reajuste anual em agosto');
    // Primeiro aceite: nada de "o contrato mudou".
    expect(text()).not.toContain('O contrato mudou');
    // Sem marcar o "li e aceito", não vai.
    expect(button('Aceitar e continuar').disabled).toBe(true);
    const cpf = document.querySelector('input[inputmode="numeric"]') as HTMLInputElement;
    const agree = document.querySelector('input[type=checkbox]') as HTMLInputElement;
    await act(async () => agree.click());
    // CPF errado: avisa na tela, sem chamar a API.
    await act(async () => type(cpf, '529.982.247-24'));
    await act(async () => button('Aceitar e continuar').click());
    expect(text()).toContain('CPF inválido: confira os números');
    expect(accepted).toEqual([]);
    await act(async () => type(cpf, '52998224725'));
    await act(async () => button('Aceitar e continuar').click());
    await flush();
    expect(accepted).toEqual([['1', '529.982.247-25']]);
    expect(text()).toContain('o painel');
  });

  it('quem aceitou a versão anterior lê o que mudou', async () => {
    previous = { version: '0', acceptedAt: '2026-09-01T15:00:00Z' } as ContractStatus['previous'];
    await render();
    expect(text()).toContain('Contrato atualizado');
    expect(text()).not.toContain('Primeiro acesso');
    expect(text()).toContain('O contrato mudou (versão 1)');
    expect(text()).toContain('Você aceitou a versão 0');
    expect(text()).toContain('Entrou o reajuste anual da mensalidade (cláusula 5).');
    expect(text()).toContain('encerrar a assinatura sem multa');
  });

  it('com o aceite (ou para a equipe), segue direto; a API pedindo o aceite traz o contrato de volta', async () => {
    required = false;
    await render();
    expect(text()).toContain('o painel');
    // Uma versão nova do contrato: a API responde CONTRACT_REQUIRED.
    required = true;
    await act(async () => {
      window.dispatchEvent(new Event(CONTRACT_REQUIRED_EVENT));
    });
    await flush();
    expect(text()).toContain('Os pontos principais');
    document.body.innerHTML = '';
    isCustomer = false;
    await render();
    expect(text()).toContain('o painel');
  });
});

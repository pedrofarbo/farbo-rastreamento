// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/components/landing/InstallersModal', () => ({
  InstallersModal: ({ isOpen }: { isOpen: boolean }) => (isOpen ? <div>lista de instaladores</div> : null),
}));

import { InstallGuidePage } from './InstallGuidePage';

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const text = () => document.body.textContent ?? '';

async function render() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  await act(async () =>
    createRoot(host).render(
      <MemoryRouter initialEntries={['/instalacao']}>
        <InstallGuidePage />
      </MemoryRouter>,
    ),
  );
}

afterEach(() => {
  document.body.innerHTML = '';
});

describe('a página de instalação do J16', () => {
  it('o que vem na caixa, a ligação do chicote e do relê, e o teste', async () => {
    await render();
    expect(document.title).toBe('Instalação do rastreador · Farbo Rastreadores');
    expect(text()).toContain('já vem configurado, com o chip de dados ativado');
    // A caixa: rastreador com chip, chicote de 4 fios e o relê de 12V.
    for (const item of ['Rastreador J16', 'Chicote de instalação', 'Mini relê de bloqueio (12V)', 'branco, amarelo e dois verdes']) {
      expect(text()).toContain(item);
    }
    // O chicote: cada fio no seu lugar.
    const rows = Array.from(document.querySelectorAll('table')[0].querySelectorAll('tbody tr')).map((tr) => tr.textContent ?? '');
    expect(rows[0]).toMatch(/Vermelho.*\+12V permanente/);
    expect(rows[1]).toMatch(/Preto.*Terra/);
    expect(rows[2]).toMatch(/Laranja.*Pós-chave/);
    expect(rows[3]).toMatch(/Amarelo.*Relê de bloqueio/);
    // O relê: branco no +12V, amarelo no rastreador, verdes em série no fio da bomba.
    const relay = Array.from(document.querySelectorAll('table')[1].querySelectorAll('tbody tr')).map((tr) => tr.textContent ?? '');
    expect(relay[0]).toMatch(/Branco.*\+12V permanente/);
    expect(relay[1]).toMatch(/Amarelo.*fio amarelo do chicote do rastreador/);
    expect(relay[2]).toMatch(/verdes.*bomba de combustível.*um verde em cada ponta/);
    // Os esquemas têm título para leitor de tela.
    const titles = Array.from(document.querySelectorAll('svg title')).map((t) => t.textContent);
    expect(titles).toEqual(['Ligação do chicote do rastreador J16', 'Ligação do mini relê de bloqueio']);
    // O relê é de 12V; o bloqueio é testado parado.
    expect(text()).toContain('relê que vem na caixa é de 12V');
    expect(text()).toContain('com o veículo parado, em local seguro');
    // As etapas têm âncoras.
    expect(Array.from(document.querySelectorAll('nav a')).map((a) => a.getAttribute('href'))).toEqual(['#local', '#chicote', '#rele', '#teste']);
    for (const id of ['local', 'chicote', 'rele', 'teste']) expect(document.getElementById(id)).not.toBeNull();
  });

  it('os instaladores parceiros e a ajuda pelo WhatsApp', async () => {
    await render();
    expect(text()).not.toContain('lista de instaladores');
    const button = Array.from(document.querySelectorAll('button')).find((b) => b.textContent === 'Ver os instaladores parceiros');
    await act(async () => button?.click());
    expect(text()).toContain('lista de instaladores');
    const whatsapp = Array.from(document.querySelectorAll('a')).find((a) => a.textContent === 'Falar com a Farbo no WhatsApp');
    expect(whatsapp?.getAttribute('href')).toContain('wa.me/');
    expect(decodeURIComponent(whatsapp?.getAttribute('href') ?? '')).toContain('Recebi o rastreador J16');
  });
});

import React from 'react';

import { WHATSAPP_NUMBER, whatsappUrl } from '@/config/contact';

import { WhatsAppIcon } from './WhatsAppFloat';
import styles from './FleetFeatures.module.css';

/** O que a frota ganha: cada item é uma funcionalidade que existe no sistema. */
export const FLEET_FEATURES: { title: string; text: string; icon: React.ReactNode }[] = [
  {
    title: 'Toda a frota no mapa',
    text: 'Centenas de veículos ao vivo no mesmo mapa, agrupados por região quando você afasta. Busca por nome, placa ou grupo.',
    icon: (
      <>
        <path d="M9 4 3 6v14l6-2 6 2 6-2V4l-6 2-6-2Z" />
        <path d="M9 4v14M15 6v14" />
      </>
    ),
  },
  {
    title: 'Grupos e campos seus',
    text: 'Organize por região, setor ou contrato. Dois campos personalizados (nº do contrato com a prefeitura, centro de custo…) para filtrar e exportar.',
    icon: (
      <>
        <rect x="3" y="3" width="7" height="7" rx="1.5" />
        <rect x="14" y="3" width="7" height="7" rx="1.5" />
        <rect x="3" y="14" width="7" height="7" rx="1.5" />
        <path d="M14 17.5h7M17.5 14v7" />
      </>
    ),
  },
  {
    title: 'Equipe com acesso sob controle',
    text: 'Cada pessoa com o próprio login: o Gestor opera, o Visualizador só acompanha. O supervisor de uma região vê só os veículos dela.',
    icon: (
      <>
        <circle cx="9" cy="8" r="3.5" />
        <path d="M2.5 20a6.5 6.5 0 0 1 13 0" />
        <path d="M16 4.5a3.5 3.5 0 0 1 0 7M18 14a6.5 6.5 0 0 1 3.5 6" />
      </>
    ),
  },
  {
    title: 'Relatórios da frota',
    text: 'Km rodados, tempo andando, parado com a ignição ligada, viagens e excessos de velocidade, por dia, veículo ou grupo, em planilha do Excel.',
    icon: <path d="M3 21h18M7 17v-5m5 5V7m5 10v-8" />,
  },
  {
    title: 'Relatório no seu e-mail',
    text: 'Toda segunda ou todo dia 1º, o resumo da semana ou do mês chega na caixa dos gestores, com a planilha completa anexa.',
    icon: (
      <>
        <rect x="3" y="5" width="18" height="14" rx="2" />
        <path d="m3 7 9 6 9-6" />
      </>
    ),
  },
  {
    title: 'Manutenção preventiva',
    text: 'Hodômetro e horímetro de cada veículo e aviso antes de vencer a troca de óleo, os pneus, a revisão ou o licenciamento, por km, horas de motor ou data.',
    icon: <path d="M14.7 6.3a4 4 0 0 0 5 5L21 13l-8 8-3-3 1.3-1.3a4 4 0 0 0-5-5L3 10l8-8 3 3-1.3 1.3Z" />,
  },
  {
    title: 'Cercas com horário',
    text: 'Desenhe a garagem, o cliente ou a obra e seja avisado na entrada e na saída, só no horário que importa (fora do expediente, por exemplo).',
    icon: (
      <>
        <path d="M12 2 3 7v10l9 5 9-5V7l-9-5Z" />
        <path d="M12 8v4l2.5 2" />
      </>
    ),
  },
  {
    title: 'Alertas que não lotam a caixa',
    text: 'Pânico, corte de energia e reboque chegam sempre na hora; o resto vem num resumo. Até 5 e-mails a mais recebem junto.',
    icon: (
      <>
        <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" />
        <path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
      </>
    ),
  },
  {
    title: 'Histórico de até 1 ano',
    text: 'O trajeto de cada veículo guardado por até 12 meses, para auditoria, contestação de multa e prestação de contas.',
    icon: (
      <>
        <circle cx="12" cy="12" r="9" />
        <path d="M12 7v5l3 2" />
      </>
    ),
  },
  {
    title: 'Uma fatura, um Pix',
    text: 'Todos os veículos vencem no mesmo dia, e a frota inteira se paga com um Pix só. Sem conferir boleto por boleto.',
    icon: (
      <>
        <rect x="5" y="3" width="14" height="18" rx="2" />
        <path d="M9 8h6M9 12h6M9 16h3" />
      </>
    ),
  },
  {
    title: 'Implantação sem trabalho',
    text: 'Mande a planilha da frota e cadastramos todos os veículos de uma vez, já nos grupos certos e com uma fatura só do equipamento.',
    icon: (
      <>
        <path d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9l-6-6Z" />
        <path d="M14 3v6h6M8 13h8M8 17h5" />
      </>
    ),
  },
  {
    title: 'Bloqueio e modo roubo',
    text: 'Bloqueio do motor pelo app (opcional) e, em caso de roubo, posição a cada 30 s com um link para mandar à polícia.',
    icon: (
      <>
        <rect x="5" y="11" width="14" height="10" rx="2" />
        <path d="M8 11V7a4 4 0 0 1 8 0v4" />
      </>
    ),
  },
];

/** A mensagem de quem pede o orçamento da frota pelo bloco das funcionalidades. */
export const FLEET_FEATURES_MESSAGE =
  'Olá! Vim pelo site da Farbo Rastreadores, vi as funcionalidades para frotas e quero um orçamento para a minha empresa.';

/**
 * Para frotas: o que a empresa ganha além do rastreamento — tudo o que o
 * sistema faz para quem tem muitos veículos —, com o pedido de orçamento.
 */
export const FleetFeatures: React.FC<{ onOpenModal: (plan?: string) => void }> = ({ onOpenModal }) => (
  <div className={styles.fleet} id="frotas">
    <div className={styles.head}>
      <span className={styles.kicker}>Para empresas e frotas</span>
      <h2 className={styles.title}>Gestão de frota completa, do mapa à manutenção</h2>
      <p className={styles.lead}>
        De 2 a milhares de veículos: tudo o que a sua operação precisa num painel só e no app, incluído na mensalidade,
        sem pagar à parte pelo sistema.
      </p>
    </div>

    <ul className={styles.grid}>
      {FLEET_FEATURES.map((feature) => (
        <li key={feature.title} className={styles.item}>
          <span className={styles.icon} aria-hidden="true">
            <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
              {feature.icon}
            </svg>
          </span>
          <h3 className={styles.itemTitle}>{feature.title}</h3>
          <p className={styles.itemText}>{feature.text}</p>
        </li>
      ))}
    </ul>

    <div className={styles.cta}>
      <div className={styles.ctaText}>
        <strong>Plataforma testada com 5.000 rastreadores ao mesmo tempo.</strong>
        <span>Mensalidade especial por volume e atendimento dedicado para a sua frota.</span>
      </div>
      {WHATSAPP_NUMBER ? (
        <a
          className={styles.ctaBtn}
          href={whatsappUrl(FLEET_FEATURES_MESSAGE)}
          target="_blank"
          rel="noopener noreferrer"
          data-analytics="frotas-funcionalidades"
        >
          <WhatsAppIcon size={20} />
          Quero um orçamento para a minha frota
        </a>
      ) : (
        <button className={styles.ctaBtn} onClick={() => onOpenModal('Frotas')} data-analytics="frotas-funcionalidades">
          Quero um orçamento para a minha frota
        </button>
      )}
    </div>
  </div>
);

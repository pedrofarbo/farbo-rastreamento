import React from 'react';

import { HIRE_VIA_WHATSAPP, WHATSAPP_NUMBER } from '@/config/contact';

import { HowItWorksArt, type StepArtKind } from './HowItWorksArt';
import styles from './HowItWorksSection.module.css';

type Actor = 'voce' | 'farbo' | 'parceiro';

const ACTOR_LABEL: Record<Actor, string> = {
  voce: 'Você',
  farbo: 'Farbo',
  parceiro: 'Prestador parceiro',
};

interface Step {
  actor: Actor;
  art: StepArtKind;
  title: string;
  description: string;
}

/**
 * O processo real, do contato ao mapa: a conta é criada pela central, o
 * pedido sai pelo painel (veículo → rastreador → assinatura), o chip e o
 * rastreador são preparados na base e enviados com rastreio, e a instalação
 * é feita e cobrada por um prestador parceiro.
 */
const STEPS: Step[] = [
  // A contratação começa pelo cadastro de interesse (com o WhatsApp ao lado),
  // ou direto pelo WhatsApp (HIRE_VIA_WHATSAPP).
  HIRE_VIA_WHATSAPP && WHATSAPP_NUMBER
    ? {
        actor: 'voce',
        art: 'whatsapp',
        title: 'Contrate pelo WhatsApp',
        description:
          'Escolha o plano e fale com a gente. Criamos a sua conta e você recebe por e-mail o convite para definir a senha do painel.',
      }
    : {
        actor: 'voce',
        art: 'interesse',
        title: 'Deixe seu interesse',
        description:
          WHATSAPP_NUMBER
            ? 'Escolha o plano e preencha o pré-cadastro, ou chame no WhatsApp. A gente responde, cria a sua conta e você recebe por e-mail o convite para definir a senha do painel.'
            : 'Escolha o plano e preencha o pré-cadastro. A gente responde por e-mail, cria a sua conta e você recebe o convite para definir a senha do painel.',
      },
  {
    actor: 'voce',
    art: 'pedido',
    title: 'Peça o rastreador pelo painel',
    description:
      'Cadastre o endereço de entrega e, em "Novo veículo", informe o carro ou a moto: rastreador e assinatura saem no mesmo pedido. O pagamento é por Pix, no próprio painel, e a mensalidade conta a partir do pedido.',
  },
  {
    actor: 'farbo',
    art: 'preparo',
    title: 'Preparamos na nossa base',
    description:
      'Separamos o chip M2M e configuramos o rastreador J16 para o seu veículo. Você acompanha cada etapa do chip e do rastreador no painel.',
  },
  {
    actor: 'farbo',
    art: 'envio',
    title: 'Enviamos até você',
    description:
      'O rastreador sai para o endereço cadastrado com código de rastreio da transportadora. Avisamos por e-mail quando ele é enviado e quando chega.',
  },
  {
    actor: 'parceiro',
    art: 'instalacao',
    title: 'Instale com um parceiro',
    description:
      'Quando o rastreador chega, o painel mostra os prestadores de instalação parceiros. Você combina horário e valor pelo WhatsApp e paga direto a eles.',
  },
  {
    actor: 'voce',
    art: 'acompanhamento',
    title: 'Acompanhe em tempo real',
    description:
      'Instalado, o rastreador se conecta sozinho. No app do celular ou no painel: mapa ao vivo, histórico de trajetos e, com o relé instalado, bloqueio do motor com o veículo parado. Os alertas — SOS, bateria desconectada, movimento com a ignição desligada, ignição ligada de madrugada e mais — chegam como notificação no celular e por e-mail.',
  },
];

export const HowItWorksSection: React.FC = () => {
  return (
    <section className={styles.section} id="como-funciona">
      <div className={styles.container}>
        <div className={styles.header}>
          <span className={styles.badge}>PASSO A PASSO</span>
          <h2 className={styles.title}>Como Funciona o Rastreamento Farbo</h2>
          <p className={styles.subtitle}>
            Do primeiro contato à instalação, você acompanha cada etapa pelo painel.
          </p>
        </div>

        <ol className={styles.stepsGrid}>
          {STEPS.map((step, index) => (
            <li key={step.title} className={styles.stepCard}>
              <div className={`${styles.art} ${styles[`art_${step.actor}`]}`}>
                <HowItWorksArt kind={step.art} />
              </div>
              <div className={styles.stepTop}>
                <span className={styles.stepNumber}>{String(index + 1).padStart(2, '0')}</span>
                <span className={`${styles.actor} ${styles[`actor_${step.actor}`]}`}>
                  {ACTOR_LABEL[step.actor]}
                </span>
              </div>
              <h3 className={styles.stepTitle}>{step.title}</h3>
              <p className={styles.stepDesc}>{step.description}</p>
            </li>
          ))}
        </ol>
      </div>
    </section>
  );
};

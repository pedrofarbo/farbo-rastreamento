import React, { useState } from 'react';
import { Link } from 'react-router-dom';

import {
  CONTACT_EMAIL,
  INSTAGRAM_HANDLE,
  INSTAGRAM_URL,
  WHATSAPP_GREETING,
  WHATSAPP_NUMBER,
  whatsappDisplay,
  whatsappUrl,
} from '@/config/contact';
import { ANATEL_HOMOLOGATION, ANATEL_LOOKUP_URL, SOCIAL_SECTION } from '@/config/landing';
import { COMPANY, CONTRACT_PATH, PRIVACY_PATH, TERMS_PATH } from '@/config/legal';

import styles from './Footer.module.css';

export const Footer: React.FC = () => {
  const [logoError, setLogoError] = useState(false);

  return (
    <footer className={styles.footer}>
      <div className={styles.container}>
        <div className={styles.topRow}>
          <div className={styles.brandCol}>
            <a href="#" className={styles.logoLink}>
              {logoError ? (
                <div className={styles.logoText}>
                  <span className={styles.brandTitle}>FARBO</span>
                  <span className={styles.brandSubtitle}>RASTREADORES</span>
                </div>
              ) : (
                <img
                  src="/assets/logo-header.png"
                  srcSet="/assets/logo-header-480.png 480w, /assets/logo-header.png 956w"
                  sizes="220px"
                  width={956}
                  height={176}
                  loading="lazy"
                  decoding="async"
                  alt="Farbo Rastreadores"
                  className={styles.logoImg}
                  onError={() => setLogoError(true)}
                />
              )}
            </a>
            <p className={styles.brandDesc}>
              Tecnologia de rastreamento veicular em tempo real para carros e motos. Liberdade com mais segurança.
            </p>
            <a
              className={styles.social}
              href={INSTAGRAM_URL}
              data-analytics="instagram"
              target="_blank"
              rel="noopener noreferrer"
              aria-label={`Instagram da Farbo Rastreadores (@${INSTAGRAM_HANDLE}), abre em nova aba`}
            >
              <svg className={styles.socialIcon} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
                <rect x="3" y="3" width="18" height="18" rx="5" />
                <circle cx="12" cy="12" r="4.2" />
                <circle className={styles.socialDot} cx="17.4" cy="6.6" r="1.1" />
              </svg>
              <span>@{INSTAGRAM_HANDLE}</span>
            </a>
          </div>

          <div className={styles.linksCol}>
            <h4>Navegação</h4>
            <ul>
              {/* Com a barra: funcionam também nas páginas dos termos e da privacidade. */}
              <li><a href="/#beneficios">Benefícios</a></li>
              <li><a href="/#planos">Planos e Preços</a></li>
              <li><a href="/#como-funciona">Como Funciona</a></li>
              <li><a href={`/#${SOCIAL_SECTION.id}`}>{SOCIAL_SECTION.label}</a></li>
            </ul>
          </div>

          <div className={styles.linksCol}>
            <h4>Regulamentação</h4>
            <ul>
              <li>
                <a
                  href={ANATEL_LOOKUP_URL}
                  target="_blank"
                  rel="noopener noreferrer"
                  title="Homologação do rastreador J16 na Anatel: confira o número na consulta pública"
                  aria-label={`Anatel ${ANATEL_HOMOLOGATION}: consultar a homologação no site da Anatel (abre em nova aba)`}
                >
                  Anatel - {ANATEL_HOMOLOGATION}
                </a>
              </li>
              {/* LGPD: o canal do titular dos dados (o encarregado). */}
              <li>
                <Link to={PRIVACY_PATH}>Privacidade e dados (LGPD)</Link>
                <span className={styles.note}>
                  <a href={`mailto:${CONTACT_EMAIL}`} title={CONTACT_EMAIL}>
                    Falar com o encarregado
                  </a>
                </span>
              </li>
              {/* CDC, art. 49: a compra fora da loja tem 7 dias para desistir. */}
              <li>
                <Link to={CONTRACT_PATH}>Arrependimento em 7 dias</Link>
                <span className={styles.note}>Após receber o rastreador (CDC, art. 49)</span>
              </li>
            </ul>
          </div>

          <div className={styles.linksCol}>
            <h4>Atendimento</h4>
            <ul>
              {WHATSAPP_NUMBER && (
                <li>
                  WhatsApp:{' '}
                  <a href={whatsappUrl(WHATSAPP_GREETING)} target="_blank" rel="noopener noreferrer" data-analytics="whatsapp">
                    {whatsappDisplay()}
                  </a>
                </li>
              )}
              <li>
                E-mail: <a href={`mailto:${CONTACT_EMAIL}`} data-analytics="email">{CONTACT_EMAIL}</a>
              </li>
              <li>Atendimento Seg-Sáb: 08h às 20h</li>
            </ul>
          </div>
        </div>

        <div className={styles.bottomBar}>
          <div className={styles.copyright}>
            <span>© {new Date().getFullYear()} Farbo Rastreadores. Todos os direitos reservados.</span>
            {/* A empresa por trás da marca: razão social e CNPJ. */}
            <span className={styles.company}>Farbo Tecnologia de Sistemas e Cloud LTDA - {COMPANY.cnpj}</span>
          </div>
          <div className={styles.legalLinks}>
            <Link to={TERMS_PATH}>Termos de Uso</Link>
            <Link to={PRIVACY_PATH}>Política de Privacidade</Link>
            <Link to={CONTRACT_PATH}>Contrato</Link>
          </div>
        </div>
      </div>
    </footer>
  );
};

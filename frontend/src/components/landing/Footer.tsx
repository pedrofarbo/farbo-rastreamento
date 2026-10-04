import React, { useState } from 'react';

import { CONTACT_EMAIL, INSTAGRAM_HANDLE, INSTAGRAM_URL, WHATSAPP_NUMBER, whatsappDisplay } from '@/config/contact';
import { ANATEL_HOMOLOGATION, SOCIAL_SECTION } from '@/config/landing';

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
              <li><a href="#beneficios">Benefícios</a></li>
              <li><a href="#planos">Planos e Preços</a></li>
              <li><a href="#como-funciona">Como Funciona</a></li>
              <li><a href={`#${SOCIAL_SECTION.id}`}>{SOCIAL_SECTION.label}</a></li>
            </ul>
          </div>

          <div className={styles.linksCol}>
            <h4>Regulamentação</h4>
            <ul>
              <li title="Homologação do rastreador J16 na Anatel">Anatel - {ANATEL_HOMOLOGATION}</li>
            </ul>
          </div>

          <div className={styles.linksCol}>
            <h4>Atendimento</h4>
            <ul>
              {WHATSAPP_NUMBER && <li>WhatsApp: {whatsappDisplay()}</li>}
              <li>
                E-mail: <a href={`mailto:${CONTACT_EMAIL}`} data-analytics="email">{CONTACT_EMAIL}</a>
              </li>
              <li>Atendimento Seg-Sáb: 08h às 20h</li>
            </ul>
          </div>
        </div>

        <div className={styles.bottomBar}>
          <span>© {new Date().getFullYear()} Farbo Rastreadores. Todos os direitos reservados.</span>
          <div className={styles.legalLinks}>
            <a href="#">Termos de Uso</a>
            <a href="#">Política de Privacidade</a>
          </div>
        </div>
      </div>
    </footer>
  );
};

import { CONTACT_EMAIL, WHATSAPP_NUMBER, whatsappDisplay } from './contact';

/**
 * A empresa nos Termos de Uso e na Política de Privacidade. Razão social,
 * CNPJ e endereço aparecem nas páginas assim que forem preenchidos (vazios,
 * ficam de fora).
 */
export const COMPANY = {
  name: 'Farbo Rastreadores',
  legalName: 'FARBO TECNOLOGIA DE SISTEMAS E CLOUD LTDA',
  cnpj: '49.757.084/0001-00',
  address: '',
  email: CONTACT_EMAIL,
  whatsapp: WHATSAPP_NUMBER ? whatsappDisplay() : '',
};

/** A data da versão em vigor dos dois documentos (mude ao alterar o texto). */
export const LEGAL_UPDATED_AT = '7 de outubro de 2026';

export const TERMS_PATH = '/termos-de-uso';
export const PRIVACY_PATH = '/politica-de-privacidade';
export const CONTRACT_PATH = '/contrato';

/**
 * Valores que os documentos citam e que vêm da configuração do servidor:
 * mude junto se mudar lá (BILLING_SUSPEND_AFTER_DAYS, retention.Options,
 * shares.MaxPerVehicle, analytics.RetentionDays).
 */
export const LEGAL_FACTS = {
  suspendAfterDays: 10,
  historyOptions: [7, 14, 30],
  historyDefault: 30,
  rawPacketsDays: 30,
  sharesPerVehicle: 5,
  analyticsDays: 400,
};

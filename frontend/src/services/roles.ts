import type { UserRole } from '@/types';

/** Os perfis de acesso como o painel mostra. */
export const ROLE_LABELS: Record<UserRole, string> = {
  admin: 'Administrador',
  operator: 'Operador',
  viewer: 'Visualização',
  customer: 'Cliente',
};

/** Os perfis da equipe da central (o cliente tem cadastro próprio, em Clientes). */
export type TeamRole = Exclude<UserRole, 'customer'>;

/** O que cada perfil da equipe pode fazer (quem garante é o servidor). */
export const TEAM_ROLES: { role: TeamRole; label: string; description: string }[] = [
  {
    role: 'admin',
    label: ROLE_LABELS.admin,
    description: 'Acesso completo: clientes, cobranças, rastreadores, prestadores e usuários.',
  },
  {
    role: 'operator',
    label: ROLE_LABELS.operator,
    description: 'Acompanha a frota, envia comandos aos veículos e cuida dos pedidos e do atendimento no WhatsApp.',
  },
  {
    role: 'viewer',
    label: ROLE_LABELS.viewer,
    description: 'Só acompanha mapa, veículos, eventos e cercas, sem alterar nada nem enviar comandos.',
  },
];

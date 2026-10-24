import type { OrgKind, OrgRole, UserRole } from '@/types';

/** Os perfis de acesso como o painel mostra. */
export const ROLE_LABELS: Record<UserRole, string> = {
  admin: 'Administrador',
  operator: 'Operador',
  viewer: 'Visualização',
  customer: 'Cliente',
  org_member: 'Associação',
};

/** Os papéis na associação ou seguradora, como a tela explica. */
export const ORG_ROLES: Record<OrgRole, { label: string; description: string }> = {
  admin: {
    label: 'Administrador',
    description: 'Faz tudo no painel: associados, veículos, bloqueio, a equipe e as faturas.',
  },
  operator: {
    label: 'Operador',
    description: 'Cadastra associados e veículos, acompanha o mapa e bloqueia e desbloqueia os veículos.',
  },
  viewer: {
    label: 'Visualizador',
    description: 'Só acompanha: mapa ao vivo, histórico, eventos e associados. Não bloqueia nem cadastra.',
  },
  broker: {
    label: 'Corretor',
    description: 'Vê e cadastra só os associados dele (e os veículos e relatórios deles). Não bloqueia nem vê o resto do painel.',
  },
};

/** "Associação" ou "Seguradora". */
export const ORG_KIND_LABELS: Record<OrgKind, string> = { association: 'Associação', insurer: 'Seguradora' };

/** Os perfis da equipe da central (o cliente tem cadastro próprio, em Clientes). */
export type TeamRole = Exclude<UserRole, 'customer' | 'org_member'>;

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

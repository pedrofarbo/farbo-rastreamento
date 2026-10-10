import { useQuery } from '@tanstack/react-query';

import { customersApi } from '@/api/resources';
import { ROLES } from '@/components/account/TeamManager';
import { Badge } from '@/components/ui/Badge';
import { Card } from '@/components/ui/Card';

import pageStyles from '../Page.module.css';

/**
 * A equipe da conta empresa do cliente, na ficha (só leitura): quem mais
 * entra na conta e com que papel. Quem muda é o titular, pelo app ou pelo
 * painel dele.
 */
export function CustomerTeamCard({ customerId }: { customerId: string }) {
  const team = useQuery({ queryKey: ['customer', customerId, 'team'], queryFn: () => customersApi.team(customerId) });
  const list = team.data ?? [];
  if (!team.data || list.length === 0) return null;
  return (
    <Card
      title="Equipe da conta"
      subtitle="Quem da empresa do cliente entra na conta, cada um com o próprio login. Quem muda é o titular."
    >
      <div className={pageStyles.form}>
        {list.map((member) => (
          <div key={member.userId} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
            <div style={{ minWidth: 0, overflowWrap: 'anywhere' }}>
              <strong>{member.name}</strong>
              <div className={pageStyles.description} style={{ margin: 0 }}>
                {member.email}
                {!member.active && ' · inativo'}
              </div>
            </div>
            <Badge tone={member.role === 'manager' ? 'success' : 'neutral'}>{ROLES[member.role].label}</Badge>
          </div>
        ))}
      </div>
    </Card>
  );
}

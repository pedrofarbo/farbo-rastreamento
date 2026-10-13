import type { PixApi } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import { Modal } from '@/components/ui/Modal';
import type { ChargeMethod, Invoice } from '@/types';

import { PixCheckout, usePixCheckout } from './PixCheckout';

interface PixPaymentModalProps {
  /** Fatura a pagar (ou um grupo delas, pela chave); nula fecha a janela. O valor habilita o cartão. */
  invoice: (Pick<Invoice, 'id' | 'description'> & { amountCents?: number }) | null;
  /** A forma que a janela abre e se a autorização do débito automático já vem marcada (a contratação no cartão). */
  initialMethod?: ChargeMethod;
  autoDebit?: boolean;
  api: PixApi;
  /** Quem está vendo: muda só o texto do fim. */
  audience: 'customer' | 'staff';
  onClose: () => void;
  /** Chamado uma vez quando o pagamento é confirmado. */
  onPaid: () => void;
}

/**
 * Pagamento de uma fatura por Pix, boleto (com Pix junto) ou cartão (o
 * cliente logado): QR Code, copia-e-cola, linha digitável, o formulário do
 * cartão e a confirmação, que aparece sozinha — a janela consulta o status
 * enquanto está aberta.
 */
export function PixPaymentModal({ invoice, api, audience, onClose, onPaid, initialMethod, autoDebit }: PixPaymentModalProps) {
  const state = usePixCheckout(invoice?.id ?? null, api, onPaid, {
    amountCents: invoice?.amountCents,
    initialMethod,
    autoDebit,
  });
  return (
    <Modal
      open={invoice !== null}
      title={state.paid ? 'Pagamento confirmado' : 'Pagar fatura'}
      onClose={onClose}
      footer={
        <Button variant={state.paid ? 'primary' : 'secondary'} onClick={onClose}>
          {state.paid ? 'Concluir' : 'Fechar'}
        </Button>
      }
    >
      <PixCheckout
        state={state}
        description={invoice?.description}
        paidText={
          audience === 'customer'
            ? 'Pagamento recebido. Obrigado! A fatura foi quitada e, se o acesso estava suspenso, ele já foi liberado.'
            : 'Pagamento confirmado pelo provedor; a fatura foi quitada automaticamente.'
        }
      />
    </Modal>
  );
}

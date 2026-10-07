import type { PixApi } from '@/api/resources';
import { Button } from '@/components/ui/Button';
import { Modal } from '@/components/ui/Modal';
import type { Invoice } from '@/types';

import { PixCheckout, usePixCheckout } from './PixCheckout';

interface PixPaymentModalProps {
  /** Fatura a pagar; nula fecha a janela. */
  invoice: Invoice | null;
  api: PixApi;
  /** Quem está vendo: muda só o texto do fim. */
  audience: 'customer' | 'staff';
  onClose: () => void;
  /** Chamado uma vez quando o pagamento é confirmado. */
  onPaid: () => void;
}

/**
 * Pagamento de uma fatura por Pix: QR Code, copia-e-cola e a confirmação,
 * que aparece sozinha — a janela consulta o status enquanto está aberta.
 */
export function PixPaymentModal({ invoice, api, audience, onClose, onPaid }: PixPaymentModalProps) {
  const state = usePixCheckout(invoice?.id ?? null, api, onPaid);
  return (
    <Modal
      open={invoice !== null}
      title={state.paid ? 'Pagamento confirmado' : 'Pagar com Pix'}
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

import type { ClaimKind, ClaimStatus, InspectionSlot, InspectionStatus } from '@/types';

// Os textos das ocorrências e das vistorias (o app do associado e o painel
// da associação).

export const CLAIM_KINDS: { id: ClaimKind; label: string }[] = [
  { id: 'collision', label: 'Colisão' },
  { id: 'theft', label: 'Furto' },
  { id: 'robbery', label: 'Roubo' },
  { id: 'glass', label: 'Vidros' },
  { id: 'natural', label: 'Fenômeno da natureza' },
  { id: 'other', label: 'Outro' },
];

export const claimKindLabel = (kind: ClaimKind) => CLAIM_KINDS.find((k) => k.id === kind)?.label ?? kind;

export const CLAIM_STATUS: Record<ClaimStatus, { label: string; tone: 'accent' | 'warning' | 'success' | 'danger' | 'neutral' }> = {
  open: { label: 'Aberta', tone: 'accent' },
  analyzing: { label: 'Em análise', tone: 'warning' },
  approved: { label: 'Aprovada', tone: 'success' },
  denied: { label: 'Negada', tone: 'danger' },
  closed: { label: 'Encerrada', tone: 'neutral' },
};

/** Ainda dá para mandar fotos e notas. */
export const claimEditable = (status: ClaimStatus) => status === 'open' || status === 'analyzing';

export const INSPECTION_SLOTS: { id: InspectionSlot; label: string; hint: string }[] = [
  { id: 'front', label: 'Frente', hint: 'O carro inteiro, de frente, com a placa legível' },
  { id: 'rear', label: 'Traseira', hint: 'O carro inteiro, de trás, com a placa legível' },
  { id: 'left', label: 'Lateral esquerda', hint: 'De lado, do para-choque da frente ao de trás' },
  { id: 'right', label: 'Lateral direita', hint: 'De lado, do para-choque da frente ao de trás' },
  { id: 'dashboard', label: 'Painel (hodômetro)', hint: 'Com o veículo ligado, mostrando a quilometragem' },
  { id: 'chassis', label: 'Número do chassi', hint: 'Gravado no vidro ou no assoalho, legível' },
];

export const INSPECTION_STATUS: Record<InspectionStatus, { label: string; tone: 'accent' | 'warning' | 'success' | 'danger' }> = {
  pending: { label: 'Esperando as fotos', tone: 'accent' },
  submitted: { label: 'Em revisão', tone: 'warning' },
  approved: { label: 'Aprovada', tone: 'success' },
  rejected: { label: 'Devolvida', tone: 'danger' },
};

/** Quantos ângulos já têm foto: "3 de 6 fotos". */
export function photosLabel(done: number): string {
  return `${done} de ${INSPECTION_SLOTS.length} fotos`;
}

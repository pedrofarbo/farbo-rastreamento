import type {
  ClaimKind,
  ClaimStatus,
  Inspection,
  InspectionKind,
  InspectionMode,
  InspectionSlot,
  InspectionSlotDef,
  InspectionStatus,
} from '@/types';

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

export const INSPECTION_STATUS: Record<InspectionStatus, { label: string; tone: 'accent' | 'warning' | 'success' | 'danger' | 'neutral' }> = {
  pending: { label: 'Esperando as fotos', tone: 'accent' },
  submitted: { label: 'Em revisão', tone: 'warning' },
  approved: { label: 'Aprovada', tone: 'success' },
  rejected: { label: 'Devolvida', tone: 'danger' },
  canceled: { label: 'Cancelada', tone: 'neutral' },
};

/** Os tipos de vistoria, na ordem do pedido. */
export const INSPECTION_KINDS: { id: InspectionKind; label: string; hint: string }[] = [
  { id: 'entry', label: 'Adesão', hint: 'Antes de o veículo entrar: todos os ângulos, o motor, o estepe e o documento.' },
  { id: 'periodic', label: 'Revistoria', hint: 'O veículo que já está na associação: os ângulos, o painel e o chassi.' },
  { id: 'claim', label: 'Sinistro', hint: 'A avaria de longe e de perto, os ângulos e o painel.' },
  { id: 'replacement', label: 'Troca de veículo', hint: 'O veículo novo do associado, como na adesão.' },
];

export const inspectionKindLabel = (kind: InspectionKind) => INSPECTION_KINDS.find((k) => k.id === kind)?.label ?? kind;

export const INSPECTION_MODES: Record<InspectionMode, { label: string; who: string }> = {
  self: { label: 'Autovistoria', who: 'O associado tira as fotos pelo link (ou pelo app).' },
  technician: { label: 'Com técnico', who: 'Um técnico tira as fotos no lugar do associado.' },
};

/** Quantos ângulos obrigatórios já têm foto: "3 de 6 fotos". */
export function photosLabel(slots: InspectionSlotDef[], photos: { slot: InspectionSlot }[]): string {
  const required = slots.filter((s) => !s.optional);
  const done = required.filter((s) => photos.some((p) => p.slot === s.id)).length;
  return `${done} de ${required.length} fotos`;
}

/** Os ângulos obrigatórios ainda sem foto. */
export function missingSlots(slots: InspectionSlotDef[], photos: { slot: InspectionSlot }[]): InspectionSlotDef[] {
  return slots.filter((s) => !s.optional && !photos.some((p) => p.slot === s.id));
}

/** O link da vistoria venceu (só a que espera as fotos). */
export function inspectionExpired(i: Pick<Inspection, 'status' | 'expiresAt'>, now = new Date()): boolean {
  return i.status === 'pending' && i.expiresAt !== null && new Date(i.expiresAt).getTime() <= now.getTime();
}

/** A mensagem para mandar o link pelo WhatsApp. */
export function inspectionMessage(
  i: Pick<Inspection, 'associateName' | 'organizationName' | 'plate' | 'vehicleName' | 'mode' | 'kind'>,
  link: string,
): string {
  const first = i.associateName.trim().split(/\s+/)[0] ?? '';
  const vehicle = i.plate || i.vehicleName;
  const kind = inspectionKindLabel(i.kind).toLowerCase();
  if (i.mode === 'technician') {
    return `Olá! Segue o link da vistoria de ${kind} do veículo ${vehicle}${first ? ` (${first})` : ''}, pela ${i.organizationName}: ${link}`;
  }
  return (
    `Olá${first ? `, ${first}` : ''}! A ${i.organizationName} pediu a vistoria de ${kind} do veículo ${vehicle}. ` +
    `Abra o link no celular, perto do veículo e com boa luz, e tire as fotos que ele pede (leva uns 10 minutos): ${link}`
  );
}

/** O endereço do WhatsApp com a mensagem (com o celular, se houver). */
export function whatsappLink(phone: string, text: string): string {
  let digits = phone.replace(/\D/g, '');
  if (digits.length === 10 || digits.length === 11) digits = `55${digits}`;
  return `https://wa.me/${digits}?text=${encodeURIComponent(text)}`;
}

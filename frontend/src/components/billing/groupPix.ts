import { meApi } from '@/api/resources';
import type { PixApi } from '@/api/resources';
import { groupIds } from '@/services/invoiceGroups';
import type { CardPayInput } from '@/types';

/**
 * O Pix, o boleto e o cartão do cliente, que também pagam várias faturas de
 * uma vez: a chave "grupo:<ids>" (groupOf) cobra todas juntas.
 */
export const meGroupPixApi: PixApi = {
  ...meApi,
  invoicePix: (key: string) => {
    const ids = groupIds(key);
    return ids ? meApi.invoicesPix(ids) : meApi.invoicePix(key);
  },
  invoiceBoleto: (key: string) => {
    const ids = groupIds(key);
    return ids ? meApi.invoicesBoleto(ids) : meApi.invoiceBoleto(key);
  },
  invoiceCard: (key: string, input: CardPayInput) => {
    const ids = groupIds(key);
    return ids ? meApi.invoicesCard(ids, input) : meApi.invoiceCard(key, input);
  },
};

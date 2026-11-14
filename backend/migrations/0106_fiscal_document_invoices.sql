-- Uma NFS-e por pagamento: as faturas pagas juntas (o Pix, o boleto ou o
-- cartão de várias, como a mensalidade de cada veículo da frota, e as baixas
-- da central seguidas) do mesmo mês saem numa nota só, com a quantidade de
-- veículos no texto. As faturas de cada nota ficam aqui (com o valor de cada
-- uma); fiscal_documents.invoice_id segue como a primeira do grupo (a
-- referência na Notaas). Uma fatura entra em uma nota só, para sempre.
CREATE TABLE fiscal_document_invoices (
    invoice_id   UUID    PRIMARY KEY REFERENCES invoices (id) ON DELETE CASCADE,
    document_id  UUID    NOT NULL REFERENCES fiscal_documents (id) ON DELETE CASCADE,
    amount_cents INTEGER NOT NULL CHECK (amount_cents > 0)
);
CREATE INDEX idx_fiscal_document_invoices_document ON fiscal_document_invoices (document_id);

INSERT INTO fiscal_document_invoices (invoice_id, document_id, amount_cents)
SELECT invoice_id, id, amount_cents FROM fiscal_documents;

-- O texto do serviço na nota (depois da descrição da configuração), montado
-- quando a nota entra na fila; vazio nas de antes: o texto da fatura.
ALTER TABLE fiscal_documents ADD COLUMN description TEXT NOT NULL DEFAULT '';

-- As notas que ainda nem chegaram à Notaas (na fila ou esperando créditos)
-- saem da fila: a rotina enfileira de novo, já juntas por pagamento. As
-- recusadas ficam como estão (a central manda de novo pela ficha).
DELETE FROM fiscal_documents WHERE status IN ('pending', 'blocked') AND provider_id = '';

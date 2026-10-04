-- positions recebe milhões de inserções e, todo dia, a limpeza do histórico
-- apaga a fatia mais antiga (~1/30 com 30 dias). Com o padrão do autovacuum
-- (20% de linhas mortas), a limpeza diária nunca chega lá: o espaço das
-- posições apagadas não é reaproveitado e a tabela incha no disco. Com 2%, o
-- vacuum roda depois de cada limpeza; as estatísticas e o mapa de
-- visibilidade (a busca da última posição pelo índice) acompanham as
-- inserções.
ALTER TABLE positions SET (
    autovacuum_vacuum_scale_factor = 0.02,
    autovacuum_analyze_scale_factor = 0.01,
    autovacuum_vacuum_insert_scale_factor = 0.05
);

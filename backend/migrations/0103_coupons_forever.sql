-- O cupom para sempre: sem o número de cobranças (charges nulo), o desconto
-- vale em todas. Ele vai para o valor da assinatura (price_cents), não para
-- o preço promocional: o reajuste anual segue valendo sobre o valor com o
-- desconto.
ALTER TABLE coupons ALTER COLUMN charges DROP NOT NULL;

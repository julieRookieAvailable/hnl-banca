-- Idempotencia del seed: el mismo tb_transfer_id nunca se inserta dos veces,
-- de modo que re-ejecutar el seed no duplica movimientos ni rompe balances.
CREATE UNIQUE INDEX IF NOT EXISTS idx_transactions_tb_transfer_id
    ON transactions(tb_transfer_id);

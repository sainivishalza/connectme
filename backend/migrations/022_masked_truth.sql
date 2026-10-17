-- Masked Truth (F2.8): the anonymous random pairing, played as truth or dare.
-- It is the same queue and the same kind of match (anonymous, ten minutes,
-- end-to-end encrypted, keep only if both agree); `style` just keeps the two
-- queues apart so someone who wants a game is not paired with someone who wants
-- a plain chat, and tells the client to open the game.
ALTER TABLE random_waiting
  ADD COLUMN style VARCHAR(10) NOT NULL DEFAULT 'chat' CHECK (style IN ('chat','truth'));
ALTER TABLE matches
  ADD COLUMN style VARCHAR(10) NOT NULL DEFAULT 'chat' CHECK (style IN ('chat','truth'));

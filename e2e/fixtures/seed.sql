-- Deterministic fixture of the end-to-end suite (e2e/README.md, design §11.6).
-- The harness (e2e/observe_test.go) knows these values: keep them in sync.
-- Test-only credentials: never reuse them anywhere else.

-- The entrypoint's client defaults to latin1, which would double-encode 'Zoé'.
SET NAMES utf8mb4;

CREATE USER 'e2e'@'%' IDENTIFIED BY 'e2e-password';
GRANT ALL PRIVILEGES ON *.* TO 'e2e'@'%';

CREATE DATABASE app;
USE app;

-- users.email holds recognizable values: a dump shows them in plaintext, or
-- none of them when the column is masked. 'Zoé' is the non-ASCII row the
-- locale scenarios filter on.
CREATE TABLE users (
  id INT PRIMARY KEY,
  email VARCHAR(100),
  name VARCHAR(100)
) CHARACTER SET utf8mb4;

INSERT INTO users (id, email, name) VALUES
  (1, 'alice@e2e.example', 'Alice'),
  (2, 'bob@e2e.example', 'Bob'),
  (3, 'zoe@e2e.example', 'Zoé');

CREATE TABLE orders (
  id INT PRIMARY KEY,
  user_id INT NOT NULL,
  amount DECIMAL(10, 2) NOT NULL,
  KEY (user_id)
) CHARACTER SET utf8mb4;

INSERT INTO orders (id, user_id, amount) VALUES
  (1, 1, 10.50),
  (2, 2, 20.00),
  (3, 3, 5.25);

CREATE VIEW e2e_user_totals AS
  SELECT u.id, SUM(o.amount) AS total
  FROM users u JOIN orders o ON o.user_id = u.id
  GROUP BY u.id;

CREATE TRIGGER e2e_orders_amount BEFORE INSERT ON orders
  FOR EACH ROW SET NEW.amount = GREATEST(NEW.amount, 0);

CREATE PROCEDURE e2e_count_users() SELECT COUNT(*) FROM users;

-- Disabled and scheduled in the future: it never changes the data.
CREATE EVENT e2e_noop_event
  ON SCHEDULE EVERY 1 DAY STARTS '2030-01-01 00:00:00'
  DISABLE
  DO DELETE FROM orders WHERE 1 = 0;

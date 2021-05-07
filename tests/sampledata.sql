INSERT INTO message_queue (data, state, task_key, task_name, created_at, created_by, last_attempt_message) VALUES
('{"order": 1}', 'READY', '1someKeyshouldbesha256', 'updateBooking', '2017-01-01 00:00:00', 'script', 'test'),
('{"order": 3}', 'READY', '1someKeyshouldbesha256', 'updateBooking', '2017-01-05 00:00:00', 'script', 'test'),
('{"order": 2}', 'READY', '1someKeyshouldbesha256', 'updateBooking', '2017-01-03 00:00:00', 'script', 'test');

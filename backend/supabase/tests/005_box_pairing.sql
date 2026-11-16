begin;
select plan(10);

select ok((select relrowsecurity and relforcerowsecurity from pg_class where oid = 'platform.pairing_codes'::regclass),
  'pairing codes enforce row level security');
select ok((select prosecdef from pg_proc where oid = 'platform.redeem_pairing_code(text,uuid,text,text,text)'::regprocedure),
  'pairing runs as a narrow definer function');
select ok(has_function_privilege('syncam_app', 'platform.redeem_pairing_code(text,uuid,text,text,text)', 'EXECUTE'),
  'the application role can redeem pairing codes');
select ok(not has_function_privilege('anon', 'platform.redeem_pairing_code(text,uuid,text,text,text)', 'EXECUTE')
  and not has_function_privilege('authenticated', 'platform.redeem_pairing_code(text,uuid,text,text,text)', 'EXECUTE'),
  'browser roles cannot redeem pairing codes');
select ok(not has_table_privilege('authenticated', 'platform.pairing_codes', 'SELECT')
  and not has_table_privilege('anon', 'platform.pairing_codes', 'SELECT'),
  'browser roles cannot read pairing code hashes');

-- Disposable synthetic fixtures, rolled back with the transaction.
insert into identity.tenants (id, name, slug) values
 ('51500000-0000-4000-8000-000000000001', 'Pairing test', 'pairing-test');
insert into config.sites (id, tenant_id, name, timezone, created_by) values
 ('51500000-0000-4000-8000-000000000011', '51500000-0000-4000-8000-000000000001', 'Client site', 'UTC', 'test');
insert into platform.pairing_codes (code_hash, tenant_id, site_id, created_by, expires_at) values
 (repeat('c', 64), '51500000-0000-4000-8000-000000000001', '51500000-0000-4000-8000-000000000011',
  'admin-1', clock_timestamp() + interval '30 minutes'),
 (repeat('d', 64), '51500000-0000-4000-8000-000000000001', '51500000-0000-4000-8000-000000000011',
  'admin-1', clock_timestamp() - interval '1 minute');

select is((select site_id::text from platform.redeem_pairing_code(repeat('c', 64),
  '51500000-0000-4000-8000-000000000021', repeat('e', 64), 'Front office PC', 'PEM-PUBLIC-KEY')),
  '51500000-0000-4000-8000-000000000011', 'a valid pairing code returns its site');
select is((select site_id::text from platform.resolve_site_key(repeat('e', 64))),
  '51500000-0000-4000-8000-000000000011', 'the box''s new site key resolves to that site');
select is((select box_public_key from platform.site_keys where id = '51500000-0000-4000-8000-000000000021'),
  'PEM-PUBLIC-KEY', 'the box public key is stored with its site key');
select is((select count(*)::int from platform.redeem_pairing_code(repeat('c', 64),
  '51500000-0000-4000-8000-000000000022', repeat('f', 64), 'Again', null)), 0,
  'a pairing code cannot be used twice');
select is((select count(*)::int from platform.redeem_pairing_code(repeat('d', 64),
  '51500000-0000-4000-8000-000000000023', repeat('1', 64), 'Late', null)), 0,
  'an expired pairing code pairs nothing');

select * from finish();
rollback;

begin;
select plan(19);

select ok((select relrowsecurity and relforcerowsecurity from pg_class where oid = 'platform.site_keys'::regclass),
  'site keys enforce row level security');
select ok((select relrowsecurity and relforcerowsecurity from pg_class where oid = 'notify.telegram_targets'::regclass),
  'telegram targets enforce row level security');
select ok((select relrowsecurity and relforcerowsecurity from pg_class where oid = 'notify.telegram_link_codes'::regclass),
  'telegram link codes enforce row level security');

select ok((select prosecdef from pg_proc where oid = 'platform.resolve_site_key(text)'::regprocedure),
  'site key lookup runs as a narrow definer function');
select ok((select prosecdef from pg_proc where oid = 'notify.redeem_telegram_link(text,bigint,text)'::regprocedure),
  'telegram link redemption runs as a narrow definer function');
select ok((select prosecdef from pg_proc where oid = 'messaging.pending_outbox_tenants()'::regprocedure),
  'pending tenant listing runs as a narrow definer function');

select ok(has_function_privilege('syncam_app', 'platform.resolve_site_key(text)', 'EXECUTE')
  and has_function_privilege('syncam_app', 'notify.redeem_telegram_link(text,bigint,text)', 'EXECUTE')
  and has_function_privilege('syncam_app', 'messaging.pending_outbox_tenants()', 'EXECUTE'),
  'the application role can call the site ingest functions');
select ok(not has_function_privilege('anon', 'platform.resolve_site_key(text)', 'EXECUTE')
  and not has_function_privilege('authenticated', 'platform.resolve_site_key(text)', 'EXECUTE'),
  'browser roles cannot probe site keys');
select ok(not has_function_privilege('anon', 'notify.redeem_telegram_link(text,bigint,text)', 'EXECUTE')
  and not has_function_privilege('authenticated', 'notify.redeem_telegram_link(text,bigint,text)', 'EXECUTE'),
  'browser roles cannot redeem telegram links');
select ok(not has_table_privilege('authenticated', 'platform.site_keys', 'SELECT')
  and not has_table_privilege('anon', 'platform.site_keys', 'SELECT'),
  'browser roles cannot read site key hashes');
select ok(not has_schema_privilege('authenticated', 'notify', 'USAGE'),
  'browser roles cannot use the notify schema');

select is((select count(*)::int from platform.resolve_site_key(repeat('0', 64))), 0,
  'an unknown key hash resolves to nothing');
select is((select count(*)::int from notify.redeem_telegram_link(repeat('0', 64), 1, 'x')), 0,
  'an unknown link code redeems nothing');
select has_column('config', 'zones', 'schedule', 'zones carry an optional schedule');

-- Disposable synthetic fixtures, rolled back with the transaction.
insert into identity.tenants (id, name, slug) values
 ('41400000-0000-4000-8000-000000000001', 'Ingest test', 'ingest-test');
insert into config.sites (id, tenant_id, name, timezone, created_by) values
 ('41400000-0000-4000-8000-000000000011', '41400000-0000-4000-8000-000000000001', 'Ingest site', 'UTC', 'test');
insert into platform.site_keys (id, tenant_id, site_id, label, key_hash, created_by) values
 ('41400000-0000-4000-8000-000000000021', '41400000-0000-4000-8000-000000000001',
  '41400000-0000-4000-8000-000000000011', 'Box', repeat('a', 64), 'test');
select is((select site_id::text from platform.resolve_site_key(repeat('a', 64))),
  '41400000-0000-4000-8000-000000000011', 'an active key resolves to its single site without tenant context');
update platform.site_keys set revoked_at = clock_timestamp() where id = '41400000-0000-4000-8000-000000000021';
select is((select count(*)::int from platform.resolve_site_key(repeat('a', 64))), 0,
  'a revoked key no longer resolves');

insert into notify.telegram_link_codes (code_hash, tenant_id, site_id, created_by, expires_at) values
 (repeat('b', 64), '41400000-0000-4000-8000-000000000001', '41400000-0000-4000-8000-000000000011',
  'test', clock_timestamp() + interval '10 minutes');
select is((select site_id::text from notify.redeem_telegram_link(repeat('b', 64), 4242, 'Team')),
  '41400000-0000-4000-8000-000000000011', 'a valid link code links its chat to the site');
select is((select count(*)::int from notify.telegram_targets where chat_id = 4242), 1,
  'the linked chat becomes a telegram target');
select is((select count(*)::int from notify.redeem_telegram_link(repeat('b', 64), 4242, 'Team')), 0,
  'a link code cannot be redeemed twice');

select * from finish();
rollback;

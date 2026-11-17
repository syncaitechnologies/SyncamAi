-- Disposable CI database only. Synthetic accounts have no passwords and every
-- fixture and temporary grant is rolled back.
begin;
select plan(22);

select ok((select proowner = 'syncam_registration_executor'::regrole and prosecdef
  and proconfig @> ARRAY['search_path=""']::text[] from pg_proc where oid =
  'identity.register_workspace(uuid,uuid,text,text,text)'::regprocedure),
  'registration runs as a narrow definer owned by its own executor');
select ok(exists (select 1 from pg_roles where rolname = 'syncam_registration_executor'
  and not rolcanlogin and not rolsuper and not rolbypassrls),
  'registration executor cannot log in or bypass RLS');
select ok(not exists (select 1 from pg_auth_members
  where roleid = 'syncam_registration_executor'::regrole and (inherit_option or set_option)),
  'nobody keeps an inherited or switchable registration executor grant');
select ok(not has_schema_privilege('syncam_registration_executor', 'identity', 'CREATE'),
  'executor cannot create identity objects');
select ok(has_function_privilege('syncam_render', 'identity.register_workspace(uuid,uuid,text,text,text)', 'EXECUTE'),
  'the API runtime role can register a workspace');
select ok(not exists (select 1 from unnest(array['anon','authenticated','service_role']) r
  where has_function_privilege(r, 'identity.register_workspace(uuid,uuid,text,text,text)', 'EXECUTE')),
  'browser and service roles cannot register a workspace directly');
select ok(not has_table_privilege('syncam_render', 'identity.user_tenant_memberships', 'INSERT')
  and not has_table_privilege('syncam_render', 'identity.tenants', 'INSERT'),
  'runtime still cannot write tenants or memberships directly');

insert into auth.users (id, email, email_confirmed_at) values
 ('41600000-0000-4000-8000-000000000001', 'signup-a@example.test', clock_timestamp()),
 ('41600000-0000-4000-8000-000000000002', 'signup-b@example.test', null),
 ('41600000-0000-4000-8000-000000000003', 'signup-c@example.test', clock_timestamp());

-- Token hook for a user with no membership: a token without the syncam claim
-- and with the server-set email flag.
select is((identity.syncam_custom_access_token(jsonb_build_object(
  'user_id', '41600000-0000-4000-8000-000000000001',
  'claims', jsonb_build_object('sub', '41600000-0000-4000-8000-000000000001',
    'app_metadata', jsonb_build_object('provider', 'email',
      'syncam', jsonb_build_object('tenant_id', 'forged'))))))->'claims'->'app_metadata'->'syncam',
  null, 'an unprovisioned user gets no syncam claim, even a forged one');
select is((identity.syncam_custom_access_token(jsonb_build_object(
  'user_id', '41600000-0000-4000-8000-000000000001',
  'claims', jsonb_build_object('sub', '41600000-0000-4000-8000-000000000001',
    'app_metadata', '{}'::jsonb))))->'claims'->'app_metadata'->'syncam_registration'->>'email_verified',
  'true', 'a confirmed email is reported to the API');
select is((identity.syncam_custom_access_token(jsonb_build_object(
  'user_id', '41600000-0000-4000-8000-000000000002',
  'claims', jsonb_build_object('sub', '41600000-0000-4000-8000-000000000002'))))
  ->'claims'->'app_metadata'->'syncam_registration'->>'email_verified',
  'false', 'an unconfirmed email is reported as unverified');

-- Run the registration as the API runtime role.
create temporary table registration_results (label text primary key, tenant_id uuid, site_id uuid, created boolean);
grant insert, select on registration_results to syncam_render;
do $$ begin
  execute format('GRANT syncam_render TO %I WITH SET TRUE', current_user);
end $$;
set local role syncam_render;
insert into registration_results
  select 'first', * from identity.register_workspace('41600000-0000-4000-8000-000000000001',
    '41600000-0000-4000-8000-000000000021', 'Acme Security Ltd.', 'Head office', 'America/Toronto');
insert into registration_results
  select 'repeat', * from identity.register_workspace('41600000-0000-4000-8000-000000000001',
    '41600000-0000-4000-8000-000000000022', 'Another name', 'Other site', 'UTC');
reset role;

select ok((select created from registration_results where label = 'first'), 'the first call creates a workspace');
select ok((select not created and tenant_id = (select tenant_id from registration_results where label = 'first')
  and site_id = (select site_id from registration_results where label = 'first')
  from registration_results where label = 'repeat'), 'a repeat call returns the same workspace');
select is((select count(*)::int from identity.tenants t join registration_results r on r.tenant_id = t.id
  where r.label = 'first' and t.name = 'Acme Security Ltd.' and t.slug like 'acme-security-ltd-%'
  and t.plan = 'trial' and t.tier = 'smb' and t.site_limit = 1 and t.camera_limit = 10
  and t.trial_ends_at between clock_timestamp() + interval '13 days' and clock_timestamp() + interval '15 days'),
  1, 'the tenant starts a 14-day trial limited to 1 site and 10 cameras');
select is((select count(*)::int from config.sites s join registration_results r on r.site_id = s.id
  where r.label = 'first' and s.name = 'Head office' and s.timezone = 'America/Toronto'),
  1, 'the first site is created');
select is((select count(*)::int from identity.user_tenant_memberships m join registration_results r on r.tenant_id = m.tenant_id
  where r.label = 'first' and m.user_id = '41600000-0000-4000-8000-000000000001'
  and m.roles = array['super_admin']::text[] and m.status = 'active' and 'users:manage' = any(m.scopes)),
  1, 'the user becomes the active Super Admin');
select is((select count(*)::int from identity.user_site_memberships m join registration_results r on r.site_id = m.site_id
  where r.label = 'first'), 1, 'the user is assigned to the first site');
select is((select count(*)::int from audit.events e join registration_results r on r.tenant_id = e.tenant_id
  where r.label = 'first'
  and canonical_payload = convert_from(canonical_payload_bytes, 'UTF8')::jsonb
  and record_hash = extensions.digest(previous_hash ||
    extensions.digest(canonical_payload_bytes, 'sha256') ||
    convert_to(canonical_payload->>'occurred_at', 'UTF8'), 'sha256')),
  3, 'three hash-verifiable audit events are written');
select is((select count(*)::int from identity.tenants where name = 'Another name'), 0,
  'a repeat call never creates a second workspace');

-- The hook now issues the usual claims plus the trial details.
select is((identity.syncam_custom_access_token(jsonb_build_object(
  'user_id', '41600000-0000-4000-8000-000000000001',
  'claims', jsonb_build_object('sub', '41600000-0000-4000-8000-000000000001', 'app_metadata', '{}'::jsonb))))
  ->'claims'->'app_metadata'->'syncam'->>'plan', 'trial', 'members receive their plan in the token');
select is((identity.syncam_custom_access_token(jsonb_build_object(
  'user_id', '41600000-0000-4000-8000-000000000001',
  'claims', jsonb_build_object('sub', '41600000-0000-4000-8000-000000000001', 'app_metadata', '{}'::jsonb))))
  ->'claims'->'app_metadata'->'syncam'->>'camera_limit', '10', 'members receive their camera limit');

update identity.user_tenant_memberships set status = 'suspended'
  where user_id = '41600000-0000-4000-8000-000000000001';
select throws_ok($$select identity.syncam_custom_access_token(jsonb_build_object(
  'user_id', '41600000-0000-4000-8000-000000000001',
  'claims', jsonb_build_object('sub', '41600000-0000-4000-8000-000000000001')))$$,
  '42501', null, 'a suspended member still cannot get a token');

set local role syncam_render;
select throws_ok($$select * from identity.register_workspace('41600000-0000-4000-8000-000000000003',
  '41600000-0000-4000-8000-000000000023', 'X', 'Site', 'UTC')$$,
  '22023', null, 'a one-character company name is rejected');
reset role;

select * from finish();
rollback;

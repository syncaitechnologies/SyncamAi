begin;
select plan(8);

select has_table('identity', 'user_tenant_memberships', 'tenant memberships exist');
select has_table('identity', 'user_site_memberships', 'site memberships exist');
select has_column('identity', 'user_tenant_memberships', 'user_id', 'membership is keyed by auth user id');
select ok(
  exists (select 1 from pg_policies where schemaname = 'identity' and tablename = 'user_tenant_memberships' and policyname = 'user_tenant_membership_self_read'),
  'members can read only themselves'
);
select ok(
  exists (select 1 from pg_policies where schemaname = 'config' and tablename = 'sites' and policyname = 'authenticated_assigned_site_read'),
  'members are restricted to assigned sites'
);
select ok(
  exists (select 1 from pg_policies where schemaname = 'identity' and tablename = 'tenants' and policyname = 'authenticated_tenant_read'),
  'members are restricted to their active tenant'
);
select function_privs_are('identity', 'syncam_custom_access_token', ARRAY['jsonb'], 'anon', ARRAY[]::text[], 'anon cannot invoke the auth hook');
select function_privs_are('identity', 'syncam_custom_access_token', ARRAY['jsonb'], 'authenticated', ARRAY[]::text[], 'browser users cannot invoke the auth hook');

select * from finish();
rollback;

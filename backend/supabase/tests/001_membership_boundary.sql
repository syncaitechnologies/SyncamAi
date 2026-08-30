begin;
select plan(8);

select has_table('identity', 'user_tenant_memberships', 'tenant memberships exist');
select has_table('identity', 'user_site_memberships', 'site memberships exist');
select has_column('identity', 'user_tenant_memberships', 'user_id', 'membership is keyed by auth user id');
select has_policy('identity', 'user_tenant_memberships', 'user_tenant_membership_self_read', 'members can read only themselves');
select has_policy('config', 'sites', 'authenticated_assigned_site_read', 'members are restricted to assigned sites');
select has_policy('identity', 'tenants', 'authenticated_tenant_read', 'members are restricted to their active tenant');
select function_privs_are('identity', 'syncam_custom_access_token', ARRAY['jsonb'], 'anon', ARRAY[]::text[], 'anon cannot invoke the auth hook');
select function_privs_are('identity', 'syncam_custom_access_token', ARRAY['jsonb'], 'authenticated', ARRAY[]::text[], 'browser users cannot invoke the auth hook');

select * from finish();
rollback;

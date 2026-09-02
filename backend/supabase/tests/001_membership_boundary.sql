begin;
select plan(27);

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
select ok(
  exists (select 1 from pg_proc where oid = 'audit.reject_event_mutation()'::regprocedure and proconfig @> ARRAY['search_path=""']::text[]),
  'audit immutability trigger has a fixed empty search path'
);
select ok(
  exists (select 1 from pg_proc where oid = 'alerts.reject_action_mutation()'::regprocedure and proconfig @> ARRAY['search_path=""']::text[]),
  'alert immutability trigger has a fixed empty search path'
);
select ok(
  exists (select 1 from pg_proc where oid = 'config.reject_privacy_mask_approval_mutation()'::regprocedure and proconfig @> ARRAY['search_path=""']::text[]),
  'privacy-mask approval immutability trigger has a fixed empty search path'
);
select ok(
  exists (select 1 from pg_proc where oid = 'config.reject_privacy_mask_release_manifest_mutation()'::regprocedure and proconfig @> ARRAY['search_path=""']::text[]),
  'privacy-mask release immutability trigger has a fixed empty search path'
);
select has_function(
  'identity',
  'bootstrap_initial_super_admin',
  ARRAY['uuid', 'uuid', 'uuid', 'text'],
  'one-time Super Admin bootstrap function exists'
);
select ok(
  exists (
    select 1 from pg_proc
    where oid = 'identity.bootstrap_initial_super_admin(uuid, uuid, uuid, text)'::regprocedure
      and prosecdef
      and proconfig @> ARRAY['search_path=""']::text[]
  ),
  'bootstrap function is private security definer with an empty search path'
);
select function_privs_are(
  'identity', 'bootstrap_initial_super_admin', ARRAY['uuid', 'uuid', 'uuid', 'text'],
  'anon', ARRAY[]::text[], 'anon cannot invoke initial Super Admin bootstrap'
);
select function_privs_are(
  'identity', 'bootstrap_initial_super_admin', ARRAY['uuid', 'uuid', 'uuid', 'text'],
  'authenticated', ARRAY[]::text[], 'browser users cannot invoke initial Super Admin bootstrap'
);
select function_privs_are(
  'identity', 'bootstrap_initial_super_admin', ARRAY['uuid', 'uuid', 'uuid', 'text'],
  'syncam_app', ARRAY[]::text[], 'application role cannot invoke initial Super Admin bootstrap'
);
select function_privs_are(
  'identity', 'bootstrap_initial_super_admin', ARRAY['uuid', 'uuid', 'uuid', 'text'],
  'service_role', ARRAY[]::text[], 'service role cannot invoke initial Super Admin bootstrap'
);
select ok(
  not has_table_privilege('syncam_app', 'identity.user_tenant_memberships', 'INSERT'),
  'application role cannot insert tenant memberships directly'
);
select ok(
  exists (
    select 1 from pg_roles
    where rolname = 'syncam_bootstrap_executor'
      and not rolcanlogin
      and not rolbypassrls
  ),
  'bootstrap executor cannot log in or bypass row-level security'
);
select ok(
  not has_schema_privilege('syncam_bootstrap_executor', 'identity', 'CREATE'),
  'bootstrap executor has no persistent schema-create privilege'
);
select has_column(
  'audit', 'events', 'canonical_payload_bytes',
  'bootstrap audit source bytes are retained for hash verification'
);
select has_table(
  'identity', 'lifecycle_delivery_requests',
  'lifecycle delivery requests are durable tenant records'
);
select ok(
  exists (select 1 from pg_policies where schemaname = 'identity' and tablename = 'lifecycle_delivery_requests' and policyname = 'lifecycle_delivery_requests_tenant_isolation'),
  'lifecycle delivery requests use transaction tenant isolation'
);
select ok(
  not has_table_privilege('authenticated', 'identity.lifecycle_delivery_requests', 'SELECT,INSERT,UPDATE,DELETE'),
  'browser users cannot read or mutate lifecycle delivery requests'
);
select ok(
  has_table_privilege('syncam_app', 'identity.lifecycle_delivery_requests', 'SELECT,INSERT,UPDATE'),
  'application role has only the lifecycle delivery privileges needed by a future worker'
);
select ok(
  position(
    'set_config(''app.tenant_id''' in pg_get_functiondef(
      'identity.bootstrap_initial_super_admin(uuid, uuid, uuid, text)'::regprocedure
    )
  ) > 0,
  'bootstrap function sets the transaction tenant context'
);

select * from finish();
rollback;

begin;
select plan(20);

select ok(exists (
  select 1 from pg_roles where rolname = 'syncam_render'
    and not rolcanlogin and rolinherit and rolconnlimit = 20
), 'fresh runtime role is disabled with a bounded connection allowance');
select ok(exists (
  select 1 from pg_roles where rolname = 'syncam_render'
    and not rolsuper and not rolcreatedb and not rolcreaterole
    and not rolreplication and not rolbypassrls
), 'runtime role cannot administer Postgres or bypass RLS');
select ok(exists (
  select 1 from pg_auth_members
  where member = 'syncam_render'::regrole and roleid = 'syncam_app'::regrole
    and inherit_option and not set_option and not admin_option
), 'runtime inherits application grants without role switching or delegation');
select is((select count(*)::int from pg_auth_members
  where member = 'syncam_render'::regrole), 1, 'runtime has only one parent role');
select ok(not pg_has_role('syncam_render', 'syncam_bootstrap_executor', 'MEMBER'),
  'runtime is not a bootstrap executor');
select ok(not has_function_privilege('syncam_render',
  'identity.bootstrap_initial_super_admin(uuid,uuid,uuid,text)', 'EXECUTE'),
  'runtime cannot create an initial administrator');
select ok(has_table_privilege('syncam_render', 'config.sites', 'SELECT')
  and has_table_privilege('syncam_render', 'config.sites', 'INSERT'),
  'runtime inherits site query and creation grants');
select ok(not has_table_privilege('syncam_render', 'identity.user_tenant_memberships', 'INSERT'),
  'runtime cannot insert memberships');
select ok(not has_table_privilege('syncam_render', 'audit.events', 'UPDATE')
  and not has_table_privilege('syncam_render', 'audit.events', 'DELETE'),
  'runtime cannot rewrite audit records');
select ok(not has_table_privilege('syncam_render', 'auth.users', 'SELECT'),
  'runtime cannot read authentication users');
select ok(not has_schema_privilege('syncam_render', 'identity', 'CREATE'),
  'runtime cannot create identity objects');
select ok(not exists (
  select 1 from pg_class where relowner in ('syncam_render'::regrole, 'syncam_app'::regrole)
), 'application and runtime roles do not own relations');
select ok(not exists (
  select 1 from pg_proc where proowner in ('syncam_render'::regrole, 'syncam_app'::regrole)
), 'application and runtime roles do not own functions');
select ok(not exists (
  select 1 from pg_class c join pg_namespace n on n.oid = c.relnamespace
  where n.nspname in ('identity', 'config', 'platform', 'audit', 'events', 'alerts', 'messaging', 'syncam_realtime', 'edge')
    and c.relkind = 'r' and (not c.relrowsecurity or not c.relforcerowsecurity)
), 'application tables retain forced row-level security');
select ok(has_database_privilege('syncam_render', current_database(), 'CONNECT'),
  'runtime can connect only after private login activation');

-- Disposable synthetic metadata only. Every fixture and temporary test grant
-- is rolled back; this file is for a fresh CI database, not a live login.
insert into identity.tenants (id, name, slug) values
 ('41100000-0000-4000-8000-000000000001', 'Runtime test A', 'runtime-test-a'),
 ('41100000-0000-4000-8000-000000000002', 'Runtime test B', 'runtime-test-b');
insert into config.sites (id, tenant_id, name, timezone, created_by) values
 ('41100000-0000-4000-8000-000000000011', '41100000-0000-4000-8000-000000000001', 'Site A', 'UTC', 'test'),
 ('41100000-0000-4000-8000-000000000012', '41100000-0000-4000-8000-000000000002', 'Site B', 'UTC', 'test');
create temporary table runtime_observations (name text primary key, passed boolean);
grant insert on runtime_observations to syncam_render;
do $$ begin
  execute format('GRANT syncam_render TO %I WITH SET TRUE', current_user);
end $$;
set local role syncam_render;
set local app.tenant_id = '';
insert into runtime_observations values ('missing_context', (select count(*) = 0 from config.sites));
set local app.tenant_id = '41100000-0000-4000-8000-000000000001';
insert into runtime_observations values ('own_tenant', (select count(*) = 1 from config.sites));
insert into runtime_observations values ('other_tenant', (select count(*) = 0 from config.sites where tenant_id = '41100000-0000-4000-8000-000000000002'));
insert into config.sites (id, tenant_id, name, timezone, created_by) values
 ('41100000-0000-4000-8000-000000000013', '41100000-0000-4000-8000-000000000001', 'Created site', 'UTC', 'test');
insert into runtime_observations values ('own_write', (select count(*) = 2 from config.sites));
do $$ begin
  begin
    insert into config.sites (id, tenant_id, name, timezone, created_by) values
     ('41100000-0000-4000-8000-000000000014', '41100000-0000-4000-8000-000000000002', 'Rejected site', 'UTC', 'test');
    insert into runtime_observations values ('foreign_write', false);
  exception when insufficient_privilege then
    insert into runtime_observations values ('foreign_write', true);
  end;
end $$;
reset role;
select ok((select passed from runtime_observations where name = 'missing_context'), 'runtime without tenant context reads no sites');
select ok((select passed from runtime_observations where name = 'own_tenant'), 'inherited grants allow own-tenant reads');
select ok((select passed from runtime_observations where name = 'other_tenant'), 'inherited RLS hides other-tenant sites');
select ok((select passed from runtime_observations where name = 'own_write'), 'inherited grants allow own-tenant inserts');
select ok((select passed from runtime_observations where name = 'foreign_write'), 'inherited RLS rejects other-tenant inserts');

select * from finish();
rollback;

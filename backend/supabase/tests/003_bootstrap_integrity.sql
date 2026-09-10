-- Disposable CI database only. Synthetic accounts have no passwords and every
-- fixture, test-only grant and fault-injection trigger is rolled back.
begin;
select plan(19);

select ok(not has_table_privilege('syncam_bootstrap_executor', 'auth.users', 'SELECT'),
  'bootstrap executor no longer reads Auth users directly');
select ok(not has_schema_privilege('syncam_bootstrap_executor', 'identity', 'CREATE'),
  'migration restores executor schema-create restriction');
select ok(not exists (
  select 1 from pg_auth_members where roleid='syncam_bootstrap_executor'::regrole
    and member='postgres'::regrole and (inherit_option or set_option)
), 'operator has no inherited or switchable bootstrap grant from any grantor');
select ok((select proowner = 'syncam_bootstrap_executor'::regrole and prosecdef
  and proconfig @> ARRAY['search_path=""']::text[] from pg_proc where oid =
  'identity.bootstrap_initial_super_admin(uuid,uuid,uuid,text)'::regprocedure),
  'function retains restricted owner and fixed search path');
select ok(not exists (select 1 from unnest(array[
  'anon','authenticated','service_role','syncam_app','syncam_render']) r
  where has_function_privilege(r,
    'identity.bootstrap_initial_super_admin(uuid,uuid,uuid,text)', 'EXECUTE')),
  'no browser or runtime role can bootstrap');

insert into auth.users (id,email) values
 ('41300000-0000-4000-8000-000000000001','bootstrap-a@example.test'),
 ('41300000-0000-4000-8000-000000000002','bootstrap-b@example.test');
insert into identity.tenants (id,name,slug) values
 ('41300000-0000-4000-8000-000000000011','Bootstrap test A','bootstrap-test-a'),
 ('41300000-0000-4000-8000-000000000012','Bootstrap test B','bootstrap-test-b');
do $$
declare runner text := current_user;
begin
  -- The test runner is deliberately no longer an inherited function owner.
  -- Authorize the test invocation as the real owner, then drop temporary role
  -- access. The invocation grant and all fixtures roll back at EOF.
  execute format('GRANT syncam_bootstrap_executor TO %I WITH SET TRUE',runner);
  execute format('GRANT syncam_bootstrap_executor TO %I WITH INHERIT FALSE',runner);
  execute 'SET LOCAL ROLE syncam_bootstrap_executor';
  execute format('GRANT EXECUTE ON FUNCTION identity.bootstrap_initial_super_admin(uuid,uuid,uuid,text) TO %I',runner);
  execute format('SET LOCAL ROLE %I',runner);
  execute format('REVOKE syncam_bootstrap_executor FROM %I GRANTED BY %I',runner,runner);
end $$;

select throws_ok($$select identity.bootstrap_initial_super_admin(
 '41300000-0000-4000-8000-000000000011','41300000-0000-4000-8000-000000000099',
 '41300000-0000-4000-8000-000000000021','TEST-BOOTSTRAP')$$,
 '23503',null,'foreign key rejects nonexistent Auth user');
select is((select count(*)::int from identity.user_tenant_memberships where
 tenant_id='41300000-0000-4000-8000-000000000011'),0,'missing user leaves no membership');
select is((select count(*)::int from audit.events where
 tenant_id='41300000-0000-4000-8000-000000000011'),0,'missing user leaves no audit event');
select throws_ok($$select identity.bootstrap_initial_super_admin(
 '41300000-0000-4000-8000-000000000099','41300000-0000-4000-8000-000000000001',
 '41300000-0000-4000-8000-000000000021','TEST-BOOTSTRAP')$$,
 '23503','tenant does not exist','nonexistent tenant is rejected');
select throws_ok($$select identity.bootstrap_initial_super_admin(
 '41300000-0000-4000-8000-000000000011','41300000-0000-4000-8000-000000000001',
 '41300000-0000-4000-8000-000000000021','')$$,
 '22023',null,'owner change reference is mandatory');
select lives_ok($$select identity.bootstrap_initial_super_admin(
 '41300000-0000-4000-8000-000000000011','41300000-0000-4000-8000-000000000001',
 '41300000-0000-4000-8000-000000000021','TEST-BOOTSTRAP')$$,
 'existing Auth user bootstraps without executor Auth read access');
select is((select count(*)::int from identity.user_tenant_memberships where
 user_id='41300000-0000-4000-8000-000000000001' and
 tenant_id='41300000-0000-4000-8000-000000000011'
 and roles=array['super_admin']::text[] and status='active'),1,
 'one active initial administrator is created');
select is((select count(*)::int from identity.user_site_memberships where
 user_id='41300000-0000-4000-8000-000000000001'),0,'bootstrap grants no site memberships');
select is((select count(*)::int from audit.events where
 tenant_id='41300000-0000-4000-8000-000000000011'
 and action='identity.super_admin.bootstrapped'
 and after_state->>'approval_reference'='TEST-BOOTSTRAP'
 and canonical_payload=convert_from(canonical_payload_bytes,'UTF8')::jsonb
 and previous_hash=decode(repeat('00',32),'hex')
 and record_hash=extensions.digest(previous_hash ||
   extensions.digest(canonical_payload_bytes,'sha256') ||
   convert_to(canonical_payload->>'occurred_at','UTF8'),'sha256')),1,
 'exactly one canonical hash-verifiable bootstrap audit event is written');
select throws_ok($$select identity.bootstrap_initial_super_admin(
 '41300000-0000-4000-8000-000000000011','41300000-0000-4000-8000-000000000001',
 '41300000-0000-4000-8000-000000000022','TEST-BOOTSTRAP')$$,
 '23505',null,'repeated bootstrap is rejected');
select throws_ok($$select identity.bootstrap_initial_super_admin(
 '41300000-0000-4000-8000-000000000011','41300000-0000-4000-8000-000000000002',
 '41300000-0000-4000-8000-000000000022','TEST-BOOTSTRAP')$$,
 '23505',null,'second administrator for the same tenant is rejected');
select throws_ok($$select identity.bootstrap_initial_super_admin(
 '41300000-0000-4000-8000-000000000012','41300000-0000-4000-8000-000000000001',
 '41300000-0000-4000-8000-000000000022','TEST-BOOTSTRAP')$$,
 '23505',null,'existing user cannot bootstrap into another tenant');

create function pg_temp.fail_bootstrap_audit() returns trigger language plpgsql
set search_path='' as $$ begin
  raise exception 'Synthetic audit failure' using errcode='P0001';
end $$;
create trigger bootstrap_test_audit_failure before insert on audit.events
for each row execute function pg_temp.fail_bootstrap_audit();
select throws_ok($$select identity.bootstrap_initial_super_admin(
 '41300000-0000-4000-8000-000000000012','41300000-0000-4000-8000-000000000002',
 '41300000-0000-4000-8000-000000000022','TEST-BOOTSTRAP')$$,
 'P0001','Synthetic audit failure','audit failure aborts bootstrap');
select is((select count(*)::int from identity.user_tenant_memberships where
 user_id='41300000-0000-4000-8000-000000000002'),0,
 'failed audit rolls back the administrator membership');

select * from finish();
rollback;

-- Disposable CI database only. Every fixture and temporary grant is rolled back.
begin;
select plan(12);

select ok((select prosecdef from pg_proc where oid = 'alerts.expired_alert_evidence(integer)'::regprocedure)
  and (select prosecdef from pg_proc where oid = 'alerts.purge_expired_alerts(uuid[])'::regprocedure),
  'retention runs through narrow definer functions');
select ok(has_function_privilege('syncam_render', 'alerts.expired_alert_evidence(integer)', 'EXECUTE')
  and has_function_privilege('syncam_render', 'alerts.purge_expired_alerts(uuid[])', 'EXECUTE'),
  'the API runtime role can run the daily cleanup');
select ok(not has_function_privilege('anon', 'alerts.purge_expired_alerts(uuid[])', 'EXECUTE')
  and not has_function_privilege('authenticated', 'alerts.purge_expired_alerts(uuid[])', 'EXECUTE')
  and not has_function_privilege('anon', 'alerts.expired_alert_evidence(integer)', 'EXECUTE')
  and not has_function_privilege('authenticated', 'alerts.expired_alert_evidence(integer)', 'EXECUTE'),
  'browser roles cannot run it');

insert into identity.tenants (id, name, slug) values
 ('41900000-0000-4000-8000-000000000001', 'Retention test', 'retention-test');
insert into config.sites (id, tenant_id, name, timezone, created_by) values
 ('41900000-0000-4000-8000-000000000011', '41900000-0000-4000-8000-000000000001', 'Shop', 'America/New_York', 'test');
insert into events.detection_events (event_id, tenant_id, dedupe_key, request_hash, occurred_at, site_id, camera_id, zone_id,
  event_type, model_version, confidence, evidence_refs, requires_human_review, payload) values
 ('41900000-0000-4000-8000-000000000101', '41900000-0000-4000-8000-000000000001', 'old', repeat('a', 64),
  clock_timestamp() - interval '7 months', '41900000-0000-4000-8000-000000000011', gen_random_uuid(), gen_random_uuid(),
  'intrusion', 'test', 0.9, '["snapshot:41900000-0000-4000-8000-000000000001/old.jpg"]', true, '{}'),
 ('41900000-0000-4000-8000-000000000102', '41900000-0000-4000-8000-000000000001', 'new', repeat('b', 64),
  clock_timestamp() - interval '1 day', '41900000-0000-4000-8000-000000000011', gen_random_uuid(), gen_random_uuid(),
  'intrusion', 'test', 0.9, '[]', true, '{}');
insert into alerts.alerts (alert_id, tenant_id, event_id, site_id, camera_id, zone_id, event_type, severity, status,
  confidence, priority, occurred_at, created_at)
select a.id, '41900000-0000-4000-8000-000000000001', a.event, '41900000-0000-4000-8000-000000000011',
  gen_random_uuid(), gen_random_uuid(), 'intrusion', 'high', a.status, 0.9, 400, a.created, a.created
from (values
  ('41900000-0000-4000-8000-000000000201'::uuid, '41900000-0000-4000-8000-000000000101'::uuid, 'dismissed', clock_timestamp() - interval '7 months'),
  ('41900000-0000-4000-8000-000000000202'::uuid, '41900000-0000-4000-8000-000000000102'::uuid, 'unacknowledged', clock_timestamp() - interval '1 day')
) as a(id, event, status, created);
insert into alerts.alert_actions (action_id, tenant_id, alert_id, action, actor_type, actor_id, request_id, payload) values
 (gen_random_uuid(), '41900000-0000-4000-8000-000000000001', '41900000-0000-4000-8000-000000000201', 'dismiss', 'user', 'test', gen_random_uuid(), '{"reason":"False positive"}');

select throws_ok($$delete from alerts.alert_actions where alert_id = '41900000-0000-4000-8000-000000000201'$$,
  '55000', null, 'action history stays append-only outside the purge');

create temporary table retention_results (step text, alert_id uuid, refs text[], removed int);
grant insert on retention_results to syncam_render;
do $$ begin
  execute format('GRANT syncam_render TO %I WITH SET TRUE', current_user);
end $$;
set local role syncam_render;
insert into retention_results (step, alert_id, refs)
  select 'expired', alert_id, evidence_refs from alerts.expired_alert_evidence(100)
  where tenant_id = '41900000-0000-4000-8000-000000000001';
insert into retention_results (step, removed) select 'purge',
  alerts.purge_expired_alerts(array['41900000-0000-4000-8000-000000000201', '41900000-0000-4000-8000-000000000202']::uuid[]);
reset role;

select is((select count(*)::int from retention_results where step = 'expired'), 1,
  'only the alert older than six months is listed');
select is((select refs from retention_results where step = 'expired'),
  array['snapshot:41900000-0000-4000-8000-000000000001/old.jpg'], 'with its snapshot reference');
select is((select removed from retention_results where step = 'purge'), 1,
  'a younger alert passed to the purge is ignored');
select is((select count(*)::int from alerts.alerts where alert_id = '41900000-0000-4000-8000-000000000201'), 0,
  'the expired alert is deleted');
select is((select count(*)::int from alerts.alert_actions where alert_id = '41900000-0000-4000-8000-000000000201'), 0,
  'with its action history');
select is((select count(*)::int from events.detection_events where event_id = '41900000-0000-4000-8000-000000000101'), 0,
  'and its detection event');
select is((select count(*)::int from alerts.alerts where alert_id = '41900000-0000-4000-8000-000000000202')
  + (select count(*)::int from events.detection_events where event_id = '41900000-0000-4000-8000-000000000102'), 2,
  'the recent alert and its event are kept');
select throws_ok($$select alerts.purge_expired_alerts(array(select gen_random_uuid() from generate_series(1, 1001)))$$,
  '22023', null, 'oversized purge batches are refused');

select * from finish();
rollback;

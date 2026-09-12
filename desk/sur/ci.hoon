::  Native CI: the %urgit <-> %urgit-ci contract, the ship-to-runner
::  channel, and the relayed `act --json` envelope.
::
::    %urgit-ci persists state-0 and nothing else in this phase; it is a
::    greenfield agent and follows the state-0 rule literally.
::
/-  git
|%
+$  candidate-id   @uv
+$  daemon-id      @uv
+$  assignment-id  @uv
+$  attempt-id     @uv
::
::  $? bunts to its last entry: a defaulted trust class is untrusted, a
::  defaulted candidate status is unknown, and a defaulted attempt status
::  is an infrastructure error.  none of them can read as success.
::
+$  trust             ?(%trusted %untrusted)
+$  result            ?(%success %failure %skipped %cancelled)
+$  candidate-status  ?(%passed %failed %pending %unknown)
+$  attempt-status    ?(%passed %failed %running %infrastructure-error)
::
::  a staged head for a CI-protected ref.  .candidate is the exact
::  integration object once %urgit has materialized it: the head itself
::  for a fast-forward, else the merge of the head onto the base tip.
::
+$  candidate
  $:  id=candidate-id
      repo=@t
      ref=@t
      head=oid:git
      base=oid:git
      candidate=(unit oid:git)
      conflict=?
      status=candidate-status
      attempts=(list attempt-id)
      created=@da
      updated=@da
  ==
::
::  a runner daemon.  a record is created when the operator mints an
::  enrollment token and activated when a daemon enrolls with it; the raw
::  token and the raw bearer are never stored, only their hashes.
::
+$  daemon
  $:  id=daemon-id
      token-hash=@
      bearer-hash=(unit @)
      minted=@da
      enrolled=(unit @da)
      last-seen=(unit @da)
  ==
::
+$  assignment
  $:  id=assignment-id
      candidate=candidate-id
      daemon=daemon-id
      attempt=attempt-id
      =trust
      deadline=@dr
      assigned=@da
      delivered=(unit @da)
  ==
::
+$  attempt-result
  $%  [%job-result =result]
      [%infrastructure-error message=@t]
  ==
::
::  one execution of a candidate on one daemon.  the event stream is not
::  stored; only the count, the recorded outputs, the relayed jobResult
::  and the claimed result enter state.
::
+$  attempt
  $:  id=attempt-id
      candidate=candidate-id
      assignment=assignment-id
      daemon=daemon-id
      =trust
      status=attempt-status
      events=@ud
      outputs=(map @t @t)
      job-result=(unit result)
      result=(unit attempt-result)
      started=@da
      finished=(unit @da)
  ==
::
+$  state-0
  $:  %0
      candidates=(map candidate-id candidate)
      daemons=(map daemon-id daemon)
      assignments=(map assignment-id assignment)
      attempts=(map attempt-id attempt)
      ci-protected=(set [repo=@t ref=@t])
  ==
::
::  the event envelope: one `act --json` line after validation
::
+$  command
  $%  [%set-output name=@t value=@t]
      [%summary body=@t]
      [%group name=@t]
      [%endgroup ~]
      [%other @t]
  ==
::
+$  event
  $:  job=@t
      job-id=@t
      stage=(unit @t)
      step=(unit @t)
      step-id=(list @t)
      step-result=(unit result)
      job-result=(unit result)
      command=(unit command)
      raw=?
      msg=@t
      at=@da
  ==
::
::  pokes on the %ci-action mark.  %urgit sends %stage-candidate,
::  %candidate-ready and %candidate-conflict; %urgit-ci sends
::  %materialize-candidate; the operator sends the rest.
::
+$  action
  $%  [%set-ci-protected repo=@t ref=@t protected=?]
      [%stage-candidate repo=@t ref=@t head=oid:git base=oid:git]
      [%materialize candidate=candidate-id]
      [%materialize-candidate repo=@t ref=@t head=oid:git base=oid:git]
      [%candidate-ready repo=@t ref=@t head=oid:git base=oid:git candidate=oid:git]
      [%candidate-conflict repo=@t ref=@t head=oid:git base=oid:git]
      [%mint-enroll-token token=@uv]
      [%assign candidate=candidate-id daemon=daemon-id deadline=(unit @dr)]
  ==
--

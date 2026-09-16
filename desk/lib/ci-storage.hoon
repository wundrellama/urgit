::  CI object-store URL signing: the existing SigV4 signer under a CI key
::  prefix that LFS cleanup never scans, namespaced by repository, run,
::  attempt and trust class.  the trust class is enforced here, at signing
::  time, not by runner cooperation.
::
/-  ci
/+  git-storage
|%
+$  settings
  (unit [credentials=credentials:git-storage configuration=configuration:git-storage])
::
::  sha-256 of an empty body: a signed GET carries no payload
::
++  empty-payload-hash
  'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855'
::
++  object-key
  |=  [repo=@t run=@t attempt=@t =trust:ci name=@t]
  ^-  @t
  (rap 3 ~['ci/' repo '/' run '/' attempt '/' trust '/' name])
::
::  an upload request for an attempt into its own trust namespace: the
::  URL plus the headers that authorize it.  this is a header-authorized
::  SigV4 request, not a presigned URL, so its lifetime is the SigV4
::  request window: the store accepts it while x-amz-date is within
::  15 minutes of its own clock.  a standalone expiring URL is later work.
::
++  sign-put
  |=  $:  =settings
          repo=@t
          run=@t
          attempt=@t
          =trust:ci
          name=@t
          content-type=@t
          payload-hash=@t
          now=@da
      ==
  ^-  (unit signed-request:git-storage)
  ?~  settings  ~
  :-  ~
  %:  sign-hash:git-storage
    'PUT'
    content-type
    payload-hash
    credentials.u.settings
    configuration.u.settings
    (object-key repo run attempt trust name)
    now
  ==
::
::  a download request, URL plus authorizing headers, with the same SigV4
::  lifetime as sign-put (x-amz-date within 15 minutes of the store's
::  clock).  refused outright when the requesting attempt's trust class
::  differs from the key's: an untrusted run never reads a trusted cache
::  and a trusted run never reads an untrusted one.
::
++  sign-get
  |=  $:  =settings
          requester=trust:ci
          repo=@t
          run=@t
          attempt=@t
          =trust:ci
          name=@t
          now=@da
      ==
  ^-  (unit signed-request:git-storage)
  ?~  settings  ~
  ?.  =(requester trust)  ~
  :-  ~
  %:  sign-hash:git-storage
    'GET'
    ''
    empty-payload-hash
    credentials.u.settings
    configuration.u.settings
    (object-key repo run attempt trust name)
    now
  ==
::
::  A browser can present a query signature when following a Location.
::  AWS's query-string SigV4 form signs host and UNSIGNED-PAYLOAD:
::  docs.aws.amazon.com/AmazonS3/latest/developerguide/sigv4-query-string-auth.html
::  Keep the P0 header-authorized arms above unchanged.
::
++  query-encode
  |=  value=@t
  ^-  @t
  =/  encoded=tape  (trip (uri-encode:git-storage value))
  %-  crip
  %-  zing
  %+  turn  encoded
  |=  char=@tD
  ?:  =(char '/')  "%2F"
  ~[char]
::
++  presign-get
  |=  $:  =settings
          requester=trust:ci
          repo=@t
          run=@t
          attempt=@t
          =trust:ci
          name=@t
          now=@da
          expires=@ud
      ==
  ^-  (unit @t)
  ?~  settings  ~
  ?.  =(requester trust)  ~
  ?.  &((gte expires 1) (lte expires 900))  ~
  =/  creds=credentials:git-storage  credentials.u.settings
  =/  config=configuration:git-storage  configuration.u.settings
  =/  host=@t  (endpoint-host:git-storage endpoint.creds)
  =/  uri=@t
    %-  uri-encode:git-storage
    (rap 3 ~['/' current-bucket.config '/' (object-key repo run attempt trust name)])
  =/  timestamp=@t  (amz-date:git-storage now)
  =/  date=@t  (date-stamp:git-storage now)
  =/  scope=@t  (rap 3 ~[date '/' region.config '/s3/aws4_request'])
  =/  query=@t
    %+  rap  3
    :~  'X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential='
        (query-encode (rap 3 ~[access-key-id.creds '/' scope]))
        '&X-Amz-Date='  timestamp
        '&X-Amz-Expires='  (scot %ud expires)
        '&X-Amz-SignedHeaders=host'
    ==
  =/  canonical=@t
    (rap 3 ~['GET\0a' uri '\0a' query '\0ahost:' host '\0a\0ahost\0aUNSIGNED-PAYLOAD'])
  =/  to-sign=@t
    %+  rap  3
    :~  'AWS4-HMAC-SHA256\0a'  timestamp  '\0a'  scope  '\0a'
        (hex-32:git-storage (shay [(met 3 canonical) canonical]))
    ==
  =/  key=@  (signing-key:git-storage secret-access-key.creds date region.config)
  =/  sig=@t  (hex-32:git-storage (hmac-text:git-storage [32 key] to-sign))
  `(rap 3 ~[(endpoint-scheme:git-storage endpoint.creds) host uri '?' query '&X-Amz-Signature=' sig])
::
::  the ship's %storage settings, read the way %urgit reads them for LFS:
::  ~ unless the agent is running and endpoint, both keys, bucket and
::  region are all set with the %credentials service.  the liveness read
::  comes first because an absent agent answers [~ ~], which no trap sees.
::
++  read-settings
  |=  [our=@p now=@da]
  ^-  settings
  =/  prefix=path  /(scot %p our)/storage/(scot %da now)
  =/  live=(unit ?)
    (mole |.(;;(? .^(* %gu (weld prefix /$)))))
  ?.  ?&(?=(^ live) u.live)  ~
  =/  found-credentials=(unit json)
    (mole |.(.^(json %gx (weld prefix /credentials/json))))
  ?~  found-credentials  ~
  =/  found-configuration=(unit json)
    (mole |.(.^(json %gx (weld prefix /configuration/json))))
  ?~  found-configuration  ~
  =/  get-string
    |=  [jon=json keys=(list @t)]
    ^-  @t
    ?~  keys  ?:(?=([%s *] jon) p.jon '')
    ?.  ?=([%o *] jon)  ''
    =/  value=(unit json)  (~(get by p.jon) i.keys)
    ?~  value  ''
    $(jon u.value, keys t.keys)
  =/  credentials=credentials:git-storage
    :*  (get-string u.found-credentials ~['storage-update' 'credentials' 'endpoint'])
        (get-string u.found-credentials ~['storage-update' 'credentials' 'accessKeyId'])
        (get-string u.found-credentials ~['storage-update' 'credentials' 'secretAccessKey'])
    ==
  =/  configuration=configuration:git-storage
    :*  (get-string u.found-configuration ~['storage-update' 'configuration' 'currentBucket'])
        (get-string u.found-configuration ~['storage-update' 'configuration' 'region'])
    ==
  =/  service=@t
    (get-string u.found-configuration ~['storage-update' 'configuration' 'service'])
  ?.  =('credentials' service)  ~
  ?.  ?&  !=('' endpoint.credentials)
          !=('' access-key-id.credentials)
          !=('' secret-access-key.credentials)
          !=('' current-bucket.configuration)
          !=('' region.configuration)
      ==
    ~
  `[credentials configuration]
--

## Whitelisting

### Configuration

Whitelists represent an operation which disables certificate trust in a certificate store. The filters presented for a whitelist are:

- `Fingerprint`: The SHA256 fingerprint of a certificate. This value will be unique across certificates given their contents are unique.

Whitelists are stored in json files. There is a basic structure to them which allows for multiple methods of whitelisting. The structure looks like:

```json
{
  "Fingerprints": [
    "050cf9fa95e40e9bddedaeda6961f6168c1279c4660172479cdd51ab03cea62c"
  ]
}
```

To apply a whitelist against a platform:

```
$ cert-manage whitelist -file wh.json
Whitelist completed successfully
```

You can also apply a whitelist against an application's certificate store:

```
$ cert-manage whitelist -file wh.json -app java
Whitelist completed successfully
```


## Generating Whitelists

Whitelists can be generated from your local browser history or a text file with urls.

```
$ cat urls.txt
https://google.com
https://yahoo.com
https://bing.com

$ cert-manage gen-whitelist -out wh.json -file urls.txt
CA                             Fingerprint      Count Example DNSNames
DigiCert Inc, www.digicert.com 7431e5f4c3c1ce46 1     yahoo.com
Baltimore, CyberTrust          16af57a9f676b0ab 1     bing.com
GeoTrust Inc.                  ff856a2d251dcd88 1     google.com

$ cat wh.json
{
    "Fingerprints": [
        "7431e5f4c3c1ce4690774f0b61e05440883ba9a01ed00ba6abd7806ed3b118cf",
        "16af57a9f676b0ab126095aa5ebadef22ab31119d644ac95cd4b93dbf3f26aeb",
        "ff856a2d251dcd88d36656f450126798cfabaade40799c722de4d2b5db36a73a"
    ]
}
```

`cert-manage` supports generating whitelists from browser history. Either all browsers `-from browser` or specific browsers `-from chrome`.

```
$ cert-manage gen-whitelist -from browser -out wh.json
CA                                             Fingerprint      Count Example DNSNames
GeoTrust Inc.                                  ff856a2d251dcd88 13    plus.google.com, accounts.google.com, hangouts.google.com
...

$ cert-manage gen-whitelist -from chrome -out wh.json
CA                                             Fingerprint      Count Example DNSNames
GeoTrust Inc.                                  ff856a2d251dcd88 13    plus.google.com, accounts.google.com, hangouts.google.com
...
```
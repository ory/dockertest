# Running dockertest in Gitlab CI
 
## How to run dockertest on shared gitlab runners?

You should add docker dind service to your job which starts in sibling container. 
That means database will be available on host `docker`.   
You app should be able to change db host through environment variable.

Here is the simple example of `gitlab-ci.yml`:
```yaml
stages:
  - test
go-test:
  stage: test
  image: golang:1.15
  services:
    - docker:dind
  variables:
    DOCKER_HOST: tcp://docker:2375
    DOCKER_DRIVER: overlay2
    YOUR_APP_DB_HOST: docker
  script:
    - go test ./...
```

Plus in the `pool.Retry` method that checks for connection readiness,
 you need to use `$YOUR_APP_DB_HOST` instead of localhost.
 

## How to run dockertest on group(custom) gitlab runners?
Gitlab runner can be run in docker executor mode to save compatibility with shared runners.    
Here is the simple register command:
```shell script
gitlab-runner register -n \
 --url https://gitlab.com/ \
 --registration-token $YOUR_TOKEN \
 --executor docker \
 --description "My Docker Runner" \
 --docker-image "docker:19.03.12" \
 --docker-privileged
```

You only need to instruct docker dind to start with disabled tls.  
Add variable `DOCKER_TLS_CERTDIR: ""` to `gitlab-ci.yml` above.
It will tell docker daemon to start on 2375 port over http. 



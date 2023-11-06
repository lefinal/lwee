# LWEE

Lightweight Workflow Execution Engine

Work in progress.

# Podman

If you want to directly use Podman, make sure you have installed Podman 4 as well as the required permissions to access _/run/podman/podman.sock_.

Keep in mind that with Podman, you need to specify the registry in image names.
Otherwise, execution might fail due to Podman trying to request a registry selection from the user.
You can avoid that by changing the following parameter in _/etc/containers/registries.conf_:

```
short-name-mode="permissive"
```

# Development

## Installing dependencies

Especially for Podman support, we need a few dependencies.
The full list of Podman dependencies can be found [here](https://podman.io/docs/installation#build-and-run-dependencies).
However, the ones described in the following should be enough.

**Ubuntu 22.04**:

```shell
# Add repo for installing Podman 4.
echo 'deb http://download.opensuse.org/repositories/devel:/kubic:/libcontainers:/unstable/xUbuntu_22.04/ /' | sudo tee /etc/apt/sources.list.d/devel:kubic:libcontainers:unstable.list
curl -fsSL https://download.opensuse.org/repositories/devel:kubic:libcontainers:unstable/xUbuntu_22.04/Release.key | gpg --dearmor | sudo tee /etc/apt/trusted.gpg.d/devel_kubic_libcontainers_unstable.gpg > /dev/null
sudo apt update
sudo apt install -y \
   podman \
   libbtrfs-dev \
   libgpgme-dev \
   libdevmapper-dev
```

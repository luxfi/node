%define _build_id_links none

Name:           node
Version:        %{version}
Release:        %{release}
Summary:        The Lux platform binaries
URL:            https://github.com/luxfi/%{name}
License:        BSD-3
AutoReqProv:    no

%description
Lux is an incredibly lightweight protocol, so the minimum computer requirements are quite modest.

%files
/usr/local/bin/node
/usr/local/lib/node
/usr/local/lib/node/evm

%changelog
* Mon Oct 26 2020 Charlie Wyse <charlie@luxlabs.org>
- First creation of package


.. _contributions:


=============
Contributions
=============

All contributions are very welcome! If you have an issue or would like to add a pull request, please visit our Github page:

https://github.com/Forceu/gokapi

Working on the upload menu
==========================

The file table exists in **two** places and they have to be changed together:

* ``internal/webserver/web/templates/html_admin.tmpl`` renders the rows present when the page loads
* ``addRow()`` in ``internal/webserver/web/static/js/admin_ui_upload.js`` builds rows for files uploaded during the session, without a reload

Changing only the template leaves freshly uploaded files looking different from the rest until the page is refreshed, which is easy to miss when testing with files that were already there.

Static assets
=============

Run ``go generate ./...`` before building: several files, including the WASM bundles, are generated and then embedded with ``go:embed``.

Minified assets are served as ``admin.min.<hash>.js`` and similar, where the hash is derived from the contents of the JS and CSS sources. Editing a script therefore changes its URL, which matters because these files are served with ``Cache-Control: max-age=36000`` — without a new URL, browsers and CDNs keep the old copy for ten hours.

Tests
=====

The test suite is behind a build tag::

    go test -tags=test ./internal/...

Without ``-tags=test`` Go reports "no test files" and exits successfully, which looks like a pass.

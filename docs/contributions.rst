.. _contributions:


=============
Contributions
=============

FGS Share is a personal fork, maintained for one instance. Its source is at
https://github.com/franco-g-s/fgs-share.

* A change that would help every Gokapi user belongs in `Gokapi <https://github.com/Forceu/Gokapi>`_ itself. It
  reaches FGS Share when upstream is merged in (see :ref:`update`).
* A security problem in either goes through private reporting, never a public issue (``SECURITY.md``).

The notes below apply to both.

Running the tests
=================

The server FGS Share runs on has no Go toolchain, so ``build/dev/gotest.sh`` runs ``go generate`` and
``go test -tags=test,awsmock`` inside the same Alpine toolchain the Dockerfile builds with, one run at a time:

.. code-block:: bash

   build/dev/gotest.sh ./...
   build/dev/gotest.sh ./internal/webserver/ ./internal/storage/

The end-to-end scripts in ``build/e2e/`` drive a throwaway container on ``127.0.0.1:53900``:

.. code-block:: bash

   docker run -d --name fgs-e2e -e GOKAPI_PORT=53900 -e GOKAPI_E2E_SEED=1 \
     -p 127.0.0.1:53900:53900 -v fgs-e2e-data:/app/data -v fgs-e2e-config:/app/config fgs-share
   python3 build/e2e/setup_local.py && docker restart fgs-e2e
   python3 build/e2e/part4_e2e.py && python3 build/e2e/groups_e2e.py \
     && python3 build/e2e/collections_e2e.py && python3 build/e2e/part3_e2e.py
   docker rm -f fgs-e2e && docker volume rm fgs-e2e-data fgs-e2e-config

The notifier's tests run on the host's Python: ``cd deploy/notifier && python3 -m unittest``.

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

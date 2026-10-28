.. _update:

======================
Updating
======================

.. note::
   Before updating, check the :ref:`changelog` for breaking changes. Some Gokapi releases require manual steps
   before or after the update.

The database schema is migrated automatically on the first start after an upgrade. It is not possible to
downgrade to an older version after a schema migration has run. FGS Share's own tables are created with
``IF NOT EXISTS`` and need no migration, so its changes alone never block a rollback (see :ref:`fgsshare`).

.. warning::
   **Back up your data before updating.** Copy the ``data`` and ``config`` directories (or their Docker volumes)
   to a safe location. With SQLite, a consistent copy of the database is made with the container stopped:

   .. code-block:: bash

      docker compose stop
      docker run --rm -v gokapi-data:/d -v "$PWD":/out alpine \
        sh -c 'apk add -q sqlite && sqlite3 /d/gokapi.sqlite ".backup /out/backup.sqlite" && sqlite3 /out/backup.sqlite "PRAGMA integrity_check"'

   The data volume must be mounted read-write even for a backup: the database uses WAL mode, and SQLite cannot
   open it from a read-only mount.


***************
Docker
***************

There is no published image to pull. Update the clone and rebuild:

.. code-block:: bash

   git pull
   docker build -t fgs-share .

Then stop the running container and start it again with the same command you used originally. Named volumes
(``-v gokapi-data:/app/data``) preserve all your data automatically.

If you use Docker Compose:

.. code-block:: bash

   git pull
   docker compose up -d --build

To make rolling back easy, give each build its own tag (for example ``fgs-share:2026-10-27``) and change the
tag in your compose file, keeping a copy of the previous one. Rolling back is then restoring that copy and
running ``docker compose up -d``.


*******************
Native deployment
*******************

.. code-block:: bash

   git pull
   make

Then restart Gokapi.


*****************************
Bringing in Gokapi's updates
*****************************

FGS Share's ``main`` branch started from Gokapi's ``master`` in July 2026 (v2.2.5-dev). Gokapi's later
fixes, including security fixes, arrive only when they are merged in:

.. code-block:: bash

   git remote add upstream https://github.com/Forceu/Gokapi.git   # once
   git fetch upstream
   git merge upstream/master

Then run the tests (:ref:`contributions`), rebuild, and update as above. Watch Gokapi's
`security advisories <https://github.com/Forceu/Gokapi/security/advisories>`_: they apply to FGS Share too
until they are merged.
